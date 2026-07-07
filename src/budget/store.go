package budget

import (
	"sync"
	"sync/atomic"
	"time"
)

// Node 预算树节点，key 是维度内容ID，用 map[string]*Node 做子节点查找 O(1)
type Node struct {
	DimCode  string           // 维度内容ID
	DimType  string           // 维度类型，从 API dimensionType 字段获取
	DimId    string           // 维度ID（dimensionId）= 表单字段名
	NodeName string           // 节点名称
	NodeID   string           // 预算节点ID（API nodeId），用于去重
	IsLeaf   bool             // 是否叶子节点
	Children map[string]*Node // 子节点，key=子节点的dimCode
}

// Tree 预算包树
//
// 每个预算包是一棵树，根节点的子节点在第1层
// 校验时沿树路径匹配：第1层→第2层→第3层，必须沿着同一分支往下走
type Tree struct {
	ID       string           // 预算包ID
	Name     string           // 预算包名称
	MaxDepth int              // 树的最大深度（含根）
	Root     map[string]*Node // 第1层节点，key=dimCode
}

// MissingTarget 记录配置中要求同步、但合思预算列表中不存在的预算包。
type MissingTarget struct {
	ID     string
	Name   string
	Reason string
}

// Store 内存缓存，按预算包存储树形结构
// 同时保留 dimCode → Node 的全局快速查找索引
type Store struct {
	mu             sync.RWMutex
	trees          []*Tree                    // 所有预算包
	index          map[string]*Node           // dimCode → 节点索引，跨所有预算包
	treeOf         map[string]*Tree           // dimCode → 所属预算包
	treeCount      map[string]int             // tree ID → 叶子节点数
	treeNodeSeen   map[string]map[string]bool // tree ID → nodeID set（去重用）
	missingTargets map[string]MissingTarget   // target ID → 未同步原因
	updatedAt      time.Time
	syncProgress   atomic.Int64 // 当前同步进度（实时）
}

func NewStore() *Store {
	return &Store{
		trees:          make([]*Tree, 0),
		index:          make(map[string]*Node),
		treeOf:         make(map[string]*Tree),
		treeCount:      make(map[string]int),
		treeNodeSeen:   make(map[string]map[string]bool),
		missingTargets: make(map[string]MissingTarget),
	}
}

// AddTree 添加一个预算包的树，并建立索引
func (s *Store) AddTree(tree *Tree) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trees = append(s.trees, tree)
	for dimCode, node := range tree.Root {
		s.index[dimCode] = node
		s.treeOf[dimCode] = tree
		s.buildIndex(node, tree)
	}
	s.updatedAt = time.Now()
}

// addTreeRef 只添加树引用，不建索引（buildTree 边建边调 indexNode）
func (s *Store) addTreeRef(tree *Tree) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trees = append(s.trees, tree)
	s.updatedAt = time.Now()
}

// indexNode 单独往索引里写一个节点，treeCount 按 nodeID 去重
func (s *Store) indexNode(dimCode string, node *Node, tree *Tree) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.index[dimCode] = node
	s.treeOf[dimCode] = tree
	if node.IsLeaf && node.NodeID != "" {
		if s.treeNodeSeen[tree.ID] == nil {
			s.treeNodeSeen[tree.ID] = make(map[string]bool)
		}
		if !s.treeNodeSeen[tree.ID][node.NodeID] {
			s.treeNodeSeen[tree.ID][node.NodeID] = true
			s.treeCount[tree.ID]++
		}
	}
	s.syncProgress.Add(1)
}

func (s *Store) buildIndex(node *Node, tree *Tree) {
	for dimCode, child := range node.Children {
		s.index[dimCode] = child
		s.treeOf[dimCode] = tree
		if child.IsLeaf && child.NodeID != "" {
			if s.treeNodeSeen[tree.ID] == nil {
				s.treeNodeSeen[tree.ID] = make(map[string]bool)
			}
			if !s.treeNodeSeen[tree.ID][child.NodeID] {
				s.treeNodeSeen[tree.ID][child.NodeID] = true
				s.treeCount[tree.ID]++
			}
		}
		s.buildIndex(child, tree)
	}
}

// FindNode 全局查找 dimCode 对应的节点
func (s *Store) FindNode(dimCode string) (*Node, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	node, ok := s.index[dimCode]
	return node, ok
}

// FindTree 查找 dimCode 所属的预算包
func (s *Store) FindTree(dimCode string) (*Tree, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tree, ok := s.treeOf[dimCode]
	return tree, ok
}

// GetTreeByName 按名称查找预算包
func (s *Store) GetTreeByName(name string) *Tree {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.trees {
		if t.Name == name {
			return t
		}
	}
	return nil
}

// GetTreeByID 按 ID 查找预算包
func (s *Store) GetTreeByID(id string) *Tree {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.trees {
		if t.ID == id {
			return t
		}
	}
	return nil
}

func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trees = make([]*Tree, 0)
	s.index = make(map[string]*Node)
	s.treeOf = make(map[string]*Tree)
	s.treeCount = make(map[string]int)
	s.treeNodeSeen = make(map[string]map[string]bool)
	s.missingTargets = make(map[string]MissingTarget)
}

// Replace 原子替换整个 store（同步用）。
// newStore 是 Sync 内部新建的私有对象，调用方保证此刻无其他 goroutine 持有它，
// 因此这里只锁 s，不再对 newStore 加锁，避免"A 锁里套 B 锁"的交叉锁隐患。
func (s *Store) Replace(newStore *Store) {
	// 先在 newStore 自己的锁下把字段读出来（防御性，未来若有外部引用也安全）
	newStore.mu.RLock()
	trees := newStore.trees
	index := newStore.index
	treeOf := newStore.treeOf
	treeCount := newStore.treeCount
	treeNodeSeen := newStore.treeNodeSeen
	missingTargets := newStore.missingTargets
	updatedAt := newStore.updatedAt
	progress := newStore.syncProgress.Load()
	newStore.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.trees = trees
	s.index = index
	s.treeOf = treeOf
	s.treeCount = treeCount
	s.treeNodeSeen = treeNodeSeen
	s.missingTargets = missingTargets
	s.updatedAt = updatedAt
	s.syncProgress.Store(progress)
}

func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.index)
}

func (s *Store) UpdatedAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.updatedAt
}

func (s *Store) Trees() []*Tree {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.trees
}

func (s *Store) ResetSyncProgress() {
	s.syncProgress.Store(0)
}

func (s *Store) IncSyncProgress() {
	s.syncProgress.Add(1)
}

func (s *Store) SyncProgress() int64 {
	return s.syncProgress.Load()
}

func (s *Store) GetTreeNodeCount(treeID string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.treeCount[treeID]
}

func (s *Store) TotalLeafCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := 0
	for _, c := range s.treeCount {
		total += c
	}
	return total
}

func (s *Store) MarkMissingTarget(target MissingTarget) {
	if target.ID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.missingTargets == nil {
		s.missingTargets = make(map[string]MissingTarget)
	}
	s.missingTargets[target.ID] = target
}

func (s *Store) MissingTarget(id string) (MissingTarget, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	target, ok := s.missingTargets[id]
	return target, ok
}

func (s *Store) MissingTargets() []MissingTarget {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]MissingTarget, 0, len(s.missingTargets))
	for _, target := range s.missingTargets {
		result = append(result, target)
	}
	return result
}

func (s *Store) HasMissingTargets() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.missingTargets) > 0
}
