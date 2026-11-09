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

// SQLiteAvailable 报告这个二进制里有没有 SQLite 驱动（doctor 用）。
//
// 没有时一切照常，只是每次启动都要重新切片（docs/02「降级」）。
func SQLiteAvailable() bool { return true }
