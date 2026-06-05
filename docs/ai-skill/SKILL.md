---
name: budget-project
description: Use when working in the BudgetProject Go repository for ekuaibao budget validation service changes, rule authoring, Windows service deployment, or release packaging.
---

# BudgetProject

## Repository Shape

- Go module at repository root.
- Source code lives in `src/`.
- Runtime config lives beside the executable: `config.yaml` or `config/config.yaml`.
- Runtime rule files live under `rules/{webhookKey}.json`.
- Release artifacts are written to `dist/`.

## Required Workflow

1. Read the current implementation before changing docs or behavior. Key files:
   - `src/main.go`
   - `src/app/app.go`
   - `src/config/config.go`
   - `src/config/loader.go`
   - `src/rules/engine.go`
   - `src/service_windows.go`
2. For any functional change or bug fix, bump `src/version.go`.
   - Bug fix: patch version.
   - Feature: minor version.
3. Build with `./build.sh`.
   - The script reads `src/version.go` and injects it with `ldflags`.
   - Do not hand-type release versions in build commands unless debugging.
4. Run `go test ./...` after rule engine, config, web, service, or packaging changes.

## Runtime Model

- On startup, the app loads config, creates a store/client/queue/checker, loads each webhook rule file, and compiles one rule engine per webhook.
- The service starts a background goroutine that performs the first budget sync, then starts queue consumption and periodic sync.
- HTTP routes are registered immediately, so webhook requests can be accepted while the first sync is still running.
- Queue consumption starts only after the first sync finishes.
- Windows service mode changes the working directory to the executable directory before loading config.

## Config Semantics

Use current config keys:

```yaml
server:
  port: 8000
ekuaibao:
  host: "https://app.ekuaibao.com"
  app_key: "..."
  app_secret: "..."
webhooks:
  budget-check:
    sign_key: "..."
    targets:
      - id: "budget-id"
        name: "budget name"
    rules: "rules/budget-check.json"
sync:
  interval_minutes: 60
  workers: 10
  queue_size: 100
web:
  enabled: true
  password: "login-password"
  admin_password: "admin-password"
dimension_names:
  E_system_costcenter: "成本中心"
  u_费用类型档案: "费用类型"
```

- `webhooks.{key}.sign_key` is the callback signing key.
- `web.admin_password` protects sync, config, rule saving, and webhook creation APIs.
- `sync.password` is obsolete.

## Rule Engine Semantics

- Each target is an independent workflow.
- Initial dataset is one `CheckUnit`: label `单据`, fields copied from the form.
- `split_detail` expands `details` and flattens `feeTypeForm`.
- `split_apportion` expands `apportions` and flattens `apportionForm`.
- `when` expressions are compiled with expr and may reference dynamic form/detail/apportion fields.
- `then: pass` removes the current unit from later steps.
- `then: commit` marks the unit committed. Later split steps keep it; later non-split steps skip it.
- `then: refuse` rejects the target and stops that target workflow.
- `match_info_to_budget` walks the budget tree by `DimId`.
- Ancestor lookup is supported for `PROJECT`, `DEPART`, and `FEE_TYPE`; other dimensions use exact match.

## Packaging

Use:

```bash
./build.sh
```

Expected release outputs:

- `dist/budget-check-windows-amd64.exe`
- `dist/budget-check-windows-arm64.exe`
- `dist/budget-check-darwin-arm64`
- `dist/budget-check-darwin-amd64`
- `dist/budget-check-linux-amd64`
- Compatibility aliases:
  - `dist/budget-check.exe`
  - `dist/budget-check-mac`

For a Windows customer deployment, ship the Windows `.exe`, `config.yaml`, and required `rules/*.json`.
