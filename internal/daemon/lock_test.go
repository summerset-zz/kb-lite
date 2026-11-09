package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

// 锁的读写与“坏锁不拦人”。
func TestLockRoundTrip(t *testing.T) {
	dir := t.TempDir()

	if _, ok := ReadLock(dir); ok {
		t.Fatal("空目录里不该有锁")
	}

	want := LockInfo{PID: os.Getpid(), Port: 7331, StartedAt: 1, Exe: "/x/kb-lite"}
	if err := WriteLock(dir, want); err != nil {
		t.Fatal(err)
	}
	got, ok := ReadLock(dir)
	if !ok || got.PID != want.PID || got.Port != want.Port {
		t.Fatalf("读回来不对：%+v", got)
	}

	RemoveLock(dir)
	if _, ok := ReadLock(dir); ok {
		t.Error("删掉之后还能读到")
	}
}

// 锁文件坏了就当没有：不能让一个半截 JSON 拦住用户启动。
func TestLockGarbageIgnored(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, LockFileName), []byte("{oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadLock(dir); ok {
		t.Error("坏锁应当当成没有")
	}
}

// AnotherInstance：活着的 pid 算“有人在跑”，死掉的 pid 不算。
func TestAnotherInstance(t *testing.T) {
	dir := t.TempDir()

	// 自己肯定活着 → 但同 pid 是“自己的锁”，不算冲突。
	_ = WriteLock(dir, LockInfo{PID: os.Getpid(), Port: 0})
	if _, running := AnotherInstance(dir); running {
		t.Error("自己的锁不该判成另一个实例")
	}

	// 一个几乎不可能存在的 pid。
	_ = WriteLock(dir, LockInfo{PID: 999999, Port: 0})
	if _, running := AnotherInstance(dir); running {
		t.Error("死掉的 pid 不该判成还在跑")
	}

	_ = os.Remove(filepath.Join(dir, LockFileName))
	if _, running := AnotherInstance(dir); running {
		t.Error("没锁的时候不该判成在跑")
	}
}
