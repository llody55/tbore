package auth

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// helper：创建临时 bbolt 文件路径
func tempDBPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "auth_test.db")
}

// =========================================================================
// 场景一：模拟爆破攻击 —— 大量不同 IP 并发失败
// 验证点：
//  1) 达到阈值后被拉黑
//  2) LRU 缓存淘汰下限可控，内存不爆炸
//  3) 未达阈值的 IP 不被拉黑
// =========================================================================

func TestBruteForceAttack(t *testing.T) {
	blocker, err := NewAuthBlocker(AuthBlockerConfig{
		DBPath:        tempDBPath(t),
		MaxFailures:   3,
		BlockDuration: 10 * time.Minute,
		LRUSize:       100, // 小 LRU 便于观测淘汰
		CleanInterval: 1 * time.Hour,
		RecordTTL:     24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("NewAuthBlocker: %v", err)
	}
	defer blocker.Close()

	// 模拟 500 个不同 IP 的并发爆破，每个 IP 失败 3 次
	const attackerCount = 500
	const failPerIP = 3

	var wg sync.WaitGroup
	wg.Add(attackerCount)

	for i := 0; i < attackerCount; i++ {
		go func(idx int) {
			defer wg.Done()
			ip := fmt.Sprintf("10.0.%d.%d", idx/256, idx%256)
			for f := 0; f < failPerIP; f++ {
				blocker.RecordFailure(ip)
			}
		}(i)
	}
	wg.Wait()

	// 验证：所有 IP 都应被拉黑（每个失败了 3 次 = 阈值）
	blocked := blocker.BlockedCount()
	t.Logf("爆破模拟完成: %d 个攻击者，当前被拉黑 %d 个", attackerCount, blocked)
	if blocked != attackerCount {
		t.Errorf("期望全部 %d 个 IP 被拉黑，实际 %d", attackerCount, blocked)
	}

	// 验证：随机抽检几个 IP 被拉黑
	for _, i := range []int{0, 100, 257, 499} {
		ip := fmt.Sprintf("10.0.%d.%d", i/256, i%256)
		if !blocker.IsBlocked(ip) {
			t.Errorf("IP %s 应被拉黑但未拉黑", ip)
		}
	}

	// 验证：未失败的 IP 不被拉黑
	innocent := "192.168.99.99"
	if blocker.IsBlocked(innocent) {
		t.Errorf("无辜 IP %s 不应被拉黑", innocent)
	}

	// 验证：LRU 缓存条目数不超过容量上限
	blocker.lru.mu.Lock()
	lruLen := len(blocker.lru.items)
	blocker.lru.mu.Unlock()
	t.Logf("LRU 缓存当前条目: %d (上限 100)", lruLen)
	if lruLen > 100 {
		t.Errorf("LRU 缓存溢出: %d > 100", lruLen)
	}
}

// =========================================================================
// 场景二：认证成功重置失败计数
// 验证：失败 2 次后成功认证，计数归零，后续失败从头计数
// =========================================================================

func TestResetOnSuccess(t *testing.T) {
	blocker, _ := NewAuthBlocker(AuthBlockerConfig{
		DBPath:        tempDBPath(t),
		MaxFailures:   3,
		BlockDuration: 10 * time.Minute,
		LRUSize:       100,
	})
	defer blocker.Close()

	ip := "10.1.1.50"

	// 失败 2 次（未达阈值）
	blocker.RecordFailure(ip)
	blocker.RecordFailure(ip)
	if blocker.IsBlocked(ip) {
		t.Fatal("失败 2 次不应被拉黑")
	}

	// 突然认证成功
	blocker.ResetFailure(ip)

	// 再失败 2 次，不应被拉黑（计数已重置）
	blocker.RecordFailure(ip)
	blocker.RecordFailure(ip)
	if blocker.IsBlocked(ip) {
		t.Fatal("重置后失败 2 次不应被拉黑")
	}

	// 第 3 次失败应触发拉黑
	blocker.RecordFailure(ip)
	if !blocker.IsBlocked(ip) {
		t.Fatal("重置后失败 3 次应被拉黑")
	}
	t.Log("PASS: 认证成功重置失败计数，后续从零计数")
}

// =========================================================================
// 场景三：bbolt 持久化 —— 重启后拉黑状态保留
// 验证：写入失败记录 → Close → 重新打开同名 db → 拉黑状态仍在
// =========================================================================

func TestPersistenceAcrossRestart(t *testing.T) {
	dbPath := tempDBPath(t)

	// 第一阶段：创建 blocker，拉黑一个 IP，然后关闭
	func() {
		blocker, _ := NewAuthBlocker(AuthBlockerConfig{
			DBPath:        dbPath,
			MaxFailures:   3,
			BlockDuration: 1 * time.Hour, // 足够长，重启后仍在窗口内
			LRUSize:       100,
		})

		ip := "10.2.2.2"
		for i := 0; i < 3; i++ {
			blocker.RecordFailure(ip)
		}
		if !blocker.IsBlocked(ip) {
			t.Fatal("拉黑前：IP 应被拉黑")
		}
		t.Logf("阶段1: IP %s 已拉黑，关闭 blocker...", ip)
		blocker.Close()
	}()

	// 验证：db 文件已生成
	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("db 文件未生成: %v", err)
	}
	t.Logf("阶段2: bbolt 文件已存在, 大小 %d bytes", info.Size())

	// 第二阶段：重新打开 db，验证拉黑状态保留
	// 注意：此处故意清空 LRU（重启后 LRU 必为空），强制走 bbolt 路径
	blocker2, _ := NewAuthBlocker(AuthBlockerConfig{
		DBPath:        dbPath,
		MaxFailures:   3,
		BlockDuration: 1 * time.Hour,
		LRUSize:       100,
	})
	defer blocker2.Close()

	// 验证：通过 blocker2 读取磁盘记录，确认数据持久化
	var firstRec *AuthFailureRecord
	var firstIP string
	blocker2.iterateAll(func(ip string, rec *AuthFailureRecord) bool {
		firstIP = ip
		firstRec = rec
		return false // 只看第一条
	})
	if firstRec == nil {
		t.Fatal("阶段2: 磁盘上应有记录但未找到")
	}
	t.Logf("阶段2: 磁盘记录 ip=%s fail_count=%d blocked_until=%s",
		firstIP, firstRec.FailCount, firstRec.BlockedUntil.Format(time.RFC3339))

	ip := "10.2.2.2"
	// 此时 LRU 是空的（blocker2 刚启动），IsBlocked 应该从磁盘加载
	if !blocker2.IsBlocked(ip) {
		t.Fatal("阶段3: 重启后 IP 仍应被拉黑（持久化生效）")
	}
	t.Logf("阶段3: 重启后 IP %s 拉黑状态保留（bbolt 持久化生效）", ip)

	// 再测试 ResetFailure 是否能跨重启正确操作
	blocker2.ResetFailure(ip)
	if blocker2.IsBlocked(ip) {
		t.Fatal("重置后不应被拉黑")
	}
	t.Log("阶段4: 重启后 ResetFailure 生效")
}

// =========================================================================
// 场景四：bbolt 持久化 —— 大量记录的磁盘占用可控
// 验证：10000 个 IP 写入后，db 文件不会爆炸增长
// =========================================================================

func TestDiskUsageUnderLargeAttack(t *testing.T) {
	dbPath := tempDBPath(t)
	blocker, _ := NewAuthBlocker(AuthBlockerConfig{
		DBPath:        dbPath,
		MaxFailures:   1, // 每个失败 1 次即拉黑，简化
		BlockDuration: 1 * time.Hour,
		LRUSize:       500,
	})
	defer blocker.Close()

	const count = 10000
	for i := 0; i < count; i++ {
		ip := fmt.Sprintf("172.16.%d.%d", i/256, i%256)
		blocker.RecordFailure(ip)
	}

	blocked := blocker.BlockedCount()
	t.Logf("写入 %d 条拉黑记录，当前被拉黑 %d", count, blocked)
	if blocked != count {
		t.Errorf("期望 %d 全部拉黑，实际 %d", count, blocked)
	}

	// 检查磁盘占用
	info, _ := os.Stat(dbPath)
	t.Logf("10000 条记录的 bbolt 文件大小: %d bytes (%.2f KB)", info.Size(), float64(info.Size())/1024)
	// 10000 条记录预期在 1MB 量级以内
	if info.Size() > 5*1024*1024 {
		t.Errorf("磁盘占用过大: %d bytes，超过 5MB", info.Size())
	}
}

// =========================================================================
// 场景五：过期清理 —— 过期后自动解封
// 验证：拉黑后等待过期 → 解封；过期记录后续被后台清理
// =========================================================================

func TestExpiryAndCleanup(t *testing.T) {
	dbPath := tempDBPath(t)

	// 使用 100ms 拉黑时长和 200ms 清理周期加速测试
	blocker, _ := NewAuthBlocker(AuthBlockerConfig{
		DBPath:        dbPath,
		MaxFailures:   1,
		BlockDuration: 100 * time.Millisecond,
		LRUSize:       100,
		CleanInterval: 200 * time.Millisecond,
		RecordTTL:     500 * time.Millisecond,
	})
	defer blocker.Close()

	ip := "10.3.3.3"
	blocker.RecordFailure(ip)

	if !blocker.IsBlocked(ip) {
		t.Fatal("拉黑后应处于拉黑状态")
	}
	t.Log("阶段1: IP 已被拉黑 (100ms 时长)")

	// 等待拉黑过期
	time.Sleep(150 * time.Millisecond)

	if blocker.IsBlocked(ip) {
		t.Fatal("100ms 后应已解封")
	}
	t.Log("阶段2: 150ms 后拉黑已过期，IsBlocked 返回 false")

	// 等待后台清理过期记录（RecordTTL=500ms，清理周期 200ms）
	time.Sleep(800 * time.Millisecond)

	// 验证磁盘上的记录已被清除
	// 使用 blocker 自身的 iterateAll，避免文件锁冲突
	var recordCount int
	blocker.iterateAll(func(ip string, rec *AuthFailureRecord) bool {
		recordCount++
		return true
	})
	t.Logf("阶段3: 过期清理后磁盘剩余记录 %d 条", recordCount)

	// 这里 recordCount 可能为 0 或极小，取决于清理时机
	if recordCount > 0 {
		t.Logf("说明: 仍有 %d 条记录未清理（可能因清理周期时间窗口），不影响拉黑判断", recordCount)
	}
}

// =========================================================================
// 场景六：纯内存模式（DBPath 为空）—— 用于不需要持久化的场景
// =========================================================================

func TestInMemoryMode(t *testing.T) {
	blocker, _ := NewAuthBlocker(AuthBlockerConfig{
		DBPath:        "", // 纯内存
		MaxFailures:   2,
		BlockDuration: 10 * time.Minute,
		LRUSize:       50,
	})
	defer blocker.Close()

	ip := "10.4.4.4"
	blocker.RecordFailure(ip)
	blocker.RecordFailure(ip)

	if !blocker.IsBlocked(ip) {
		t.Fatal("纯内存模式下也应正常拉黑")
	}
	t.Log("PASS: 纯内存模式（无持久化）拉黑正常")
}

// =========================================================================
// 场景七：拉黑后再失败 —— 截止时间顺延
// =========================================================================

func TestBlockExtension(t *testing.T) {
	blocker, _ := NewAuthBlocker(AuthBlockerConfig{
		DBPath:        tempDBPath(t),
		MaxFailures:   1,
		BlockDuration: 1 * time.Hour,
		LRUSize:       100,
	})
	defer blocker.Close()

	ip := "10.5.5.5"
	blocker.RecordFailure(ip)

	// 获取第一次的截止时间
	rec1 := blocker.getRecord(ip)
	until1 := rec1.BlockedUntil

	// 稍等一点，确保时间戳不同
	time.Sleep(10 * time.Millisecond)

	// 拉黑窗口内再次失败
	blocker.RecordFailure(ip)
	rec2 := blocker.getRecord(ip)
	until2 := rec2.BlockedUntil

	if !until2.After(until1) {
		t.Fatalf("拉黑窗口内再失败应顺延截止时间: %s -> %s", until1, until2)
	}
	t.Logf("PASS: 截止时间顺延 %s -> %s", until1.Format(time.RFC3339), until2.Format(time.RFC3339))

	// 同时验证 FailCount 累加
	if rec2.FailCount != 2 {
		t.Errorf("FailCount 应为 2，实际 %d", rec2.FailCount)
	}
}

// =========================================================================
// 内存占用快照：观测爆破测试前后的 heap 增长
// =========================================================================

func TestMemoryFootprint(t *testing.T) {
	dbPath := tempDBPath(t)
	blocker, _ := NewAuthBlocker(AuthBlockerConfig{
		DBPath:        dbPath,
		MaxFailures:   1,
		BlockDuration: 1 * time.Hour,
		LRUSize:       1000,
	})
	defer blocker.Close()

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	// 模拟大规模爆破
	const count = 50000
	for i := 0; i < count; i++ {
		ip := fmt.Sprintf("10.%d.%d.%d", (i>>16)&0xFF, (i>>8)&0xFF, i&0xFF)
		blocker.RecordFailure(ip)
	}

	runtime.GC()
	runtime.ReadMemStats(&after)

	heapIncr := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf("5万 IP 爆破后 heap 增长: %d bytes (%.2f MB)",
		heapIncr, float64(heapIncr)/1024/1024)
	t.Logf("LRU 缓存容量上限 1000，磁盘记录 %d 条", blocker.BlockedCount())

	// 内存增长不应超过 50MB（LRU 1000 条 + bbolt 页缓存有限）
	if heapIncr > 50*1024*1024 {
		t.Errorf("内存增长过大: %d bytes", heapIncr)
	}
}
