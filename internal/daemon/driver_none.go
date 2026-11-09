//go:build nosqlite

package daemon

import "database/sql"

// openSQLite 在 `-tags nosqlite` 的构建里直接失败，上层退回纯内存模式
// （docs/08：驱动不可用时仍然可用，只是每次启动要重切）。
func openSQLite(string) (*sql.DB, error) {
	return nil, ErrDBUnavailable
}
