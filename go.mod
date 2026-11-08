module github.com/summerset-zz/kb-lite

// 1.22 是底线：net/http 的 ServeMux 增强路由（方法匹配 + {path...}）从 1.22 才有，
// 见 docs/08-build-release.md。不要因为本机版本高就往上抬。
go 1.22.0

require gopkg.in/yaml.v3 v3.0.1 // indirect
