package budget

import (
	"sync"
	"time"
)

// Node 预算树节点，key 是维度内容ID，用 map[string]*Node 做子节点查找 O(1)
type Node struct {
	 DimCode  string                // 维度内容ID
	 NodeName string                // 节点名称
	 IsLeaf   bool                  // 是否叶子节点
	 Children  map[string]*Node     // 子节点，key=子节点的dimCode
}

// Tree 预算包树
//
// 每个预算包是一棵树，根节点的子节点在第1层
// 同一层的所有节点共享同一个维度类型
// 校验时沿树路径匹配：第1层→第2层→第3层，必须沿着同一分支往下走
//
// 示例:
//
//	Tree: 2026成本中心预算包
//	  Level 1 DEPARTMENT (Root的子节点):
//	    "ID001"(销售部) → Level 2 ARCHIVE:
//	                       "ID101"(差旅费) → Level 3 PROJECT: "ID201"(项目A)
//	                       "ID102"(办公费) → Level 3 PROJECT: "ID202"(项目B)
//	    "ID002"(研发部) → Level 1 ARCHIVE:
//	                       "ID103"(设备费) → Level 2 PROJECT: "ID203"(项目C)
type Tree struct {
	ID       string               // 预算包ID
	Name     string               // 预算包名称
	DimType  string               // 根节点下一层的维度类型（DEPARTMENT/ARCHIVE/PROJECT）
	MaxDepth int                  // 树的最大深度（含根）
	Root     map[string]*Node     // 第1层节点，key=dimCode
}

// Store 内存缓存，按预算包存储树形结构
// 同时保留 dimCode → Node 的全局快速查找索引
type Store struct {
	mu        sync.RWMutex
	trees     []*Tree            // 所有预算包
	index     map[string]*Node   // dimCode → 节点索引，跨所有预算包
	treeOf    map[string]*Tree   // dimCode → 所属预算包
	updatedAt time.Time
}

func NewStore() *Store {
	return &Store{
		trees:  make([]*Tree, 0),
		index:  make(map[string]*Node),
		treeOf: make(map[string]*Tree),
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

func (s *Store) buildIndex(node *Node, tree *Tree) {
	for dimCode, child := range node.Children {
		s.index[dimCode] = child
		s.treeOf[dimCode] = tree
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

func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trees = make([]*Tree, 0)
	s.index = make(map[string]*Node)
	s.treeOf = make(map[string]*Tree)
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