//go:build !nosqlite

package daemon

import (
	"database/sql"

	// 纯 Go 的 SQLite（无 cgo），见 docs/08 与 docs/10 ADR-013。
	// **不要**换成 mattn/go-sqlite3：它要 cgo，五个平台的交叉编译会立刻全废。
	_ "modernc.org/sqlite"
)

// openSQLite 打开驱动。默认构建就带它；只有 `-tags nosqlite` 的瘦身构建没有。
func openSQLite(path string) (*sql.DB, error) {
	return sql.Open("sqlite", path)
}
