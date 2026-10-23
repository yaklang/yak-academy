package api

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
	"github.com/yaklang/yak-academy/backend/internal/service"
)

// 契约防线（api-contract skill 的机械执行）：docs/openapi.yaml 声明的
// (method, path) 集合与实际注册路由双向一致——契约有而路由无 = 未实现，
// 路由有而契约无 = 私加接口；同时校验 admin tag ↔ 注册角色、
// security: [] ↔ 公开路由，防止鉴权声明在两侧漂移。

var httpMethods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true, "patch": true,
}

type contractOp struct {
	admin  bool // tags 含 admin
	public bool // 显式 security: []（覆盖全局 bearerAuth）
}

var pathWildcard = regexp.MustCompile(`\{[^}]+\}`)

// normalizeContractKey 把路由模式与契约 path 归一到同一形态：
// 去掉 /api/v1 前缀（servers.url 已声明），通配符 {name} 归一为 {}。
func normalizeContractKey(method, path string) string {
	path = strings.TrimPrefix(path, "/api/v1")
	return method + " " + pathWildcard.ReplaceAllString(path, "{}")
}

// loadContract 从 docs/openapi.yaml 收集全部操作：key → admin/public 语义。
func loadContract(t *testing.T) map[string]contractOp {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "openapi.yaml"))
	if err != nil {
		t.Fatalf("读取契约文件: %v", err)
	}
	var doc struct {
		Paths map[string]map[string]yaml.Node `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("解析契约 YAML: %v", err)
	}
	ops := make(map[string]contractOp)
	for path, item := range doc.Paths {
		for key, node := range item {
			if !httpMethods[key] {
				continue // path 级 parameters 等非操作键
			}
			var op struct {
				Tags     []string  `yaml:"tags"`
				Security yaml.Node `yaml:"security"`
			}
			if err := node.Decode(&op); err != nil {
				t.Fatalf("解析操作 %s %s: %v", key, path, err)
			}
			var co contractOp
			for _, tag := range op.Tags {
				if tag == "admin" {
					co.admin = true
				}
			}
			// 契约全局 security: [bearerAuth]，仅显式 security: [] 的操作是公开的。
			co.public = op.Security.Kind == yaml.SequenceNode && len(op.Security.Content) == 0
			ops[normalizeContractKey(strings.ToUpper(key), path)] = co
		}
	}
	if len(ops) == 0 {
		t.Fatal("契约未解析到任何操作（paths 为空？）")
	}
	return ops
}

func TestContractMatchesRegisteredRoutes(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open("file:apicontracttest?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Migrate(gdb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := gdb.DB()
		sqlDB.Close()
	})
	auth := service.NewAuthService(repo.NewUserRepo(gdb), "test-secret")
	router := NewRouter(auth)
	// nil 服务即可：Register 只做路由注册，不触达业务。
	NewHandlers(auth, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false).Register(router)

	contract := loadContract(t)
	registered := make(map[string]RouteInfo, len(router.Routes()))
	for _, ri := range router.Routes() {
		key := normalizeContractKey(ri.Method, ri.Pattern)
		if _, dup := registered[key]; dup {
			t.Errorf("路由重复注册: %s", key)
			continue
		}
		registered[key] = ri
	}

	for key, co := range contract {
		ri, ok := registered[key]
		if !ok {
			t.Errorf("契约声明但路由未注册: %s", key)
			continue
		}
		if co.admin != (ri.Role == model.GlobalRoleAdmin) {
			t.Errorf("%s: 契约 admin=%v 但路由角色=%q（student 端可达的 admin 接口是事故）", key, co.admin, ri.Role)
		}
		if co.public != ri.Public {
			t.Errorf("%s: 契约 security:[]=%v 但路由 Public=%v（公开声明漂移）", key, co.public, ri.Public)
		}
	}
	for key := range registered {
		if _, ok := contract[key]; !ok {
			t.Errorf("路由已注册但契约未声明: %s（先改 openapi.yaml，api-contract skill）", key)
		}
	}
	if t.Failed() {
		t.Logf("契约 %d 个操作 vs 路由 %d 条", len(contract), len(registered))
	}
}
