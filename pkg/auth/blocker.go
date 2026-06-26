package auth

import (
	"container/list"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.etcd.io/bbolt"
)

// 默认参数，零配置也能安全运行
const (
	defaultMaxFailures   = 3
	defaultBlockDuration = 10 * time.Minute
	defaultLRUSize       = 1000
	defaultCleanInterval = 1 * time.Minute
	// 历史/过期记录最长保留时间，超过即从磁盘删除，防止 db 文件无限增长
	defaultRecordTTL = 24 * time.Hour

	bucketName = "auth_failures"
)

// AuthFailureRecord 描述单个 IP 的认证失败与拉黑状态
type AuthFailureRecord struct {
	FailCount    int       `json:"fail_count"`
	LastFailAt   time.Time `json:"last_fail_at"`
	BlockedUntil time.Time `json:"blocked_until"`
}

// IsBlocked 判断当前是否处于拉黑窗口内
func (r *AuthFailureRecord) IsBlocked(now time.Time) bool {
	return !r.BlockedUntil.IsZero() && now.Before(r.BlockedUntil)
}

// AuthBlockerConfig 控制 AuthBlocker 的行为，所有字段都有零值默认
type AuthBlockerConfig struct {
	// DBPath bbolt 文件路径；为空则禁用持久化（纯内存模式，仅用于测试）
	DBPath string
	// MaxFailures 触发拉黑所需的连续失败次数，<=0 使用默认值 3
	MaxFailures int
	// BlockDuration 每次拉黑持续时长，<=0 使用默认值 10 分钟
	BlockDuration time.Duration
	// LRUSize 内存 LRU 缓存的条目上限，<=0 使用默认值 1000
	LRUSize int
	// CleanInterval 后台清理过期记录的周期，<=0 使用默认值 1 分钟
	CleanInterval time.Duration
	// RecordTTL 历史/过期记录在磁盘上的最长保留时间，<=0 使用默认值 24 小时
	RecordTTL time.Duration
}

// AuthBlocker 封装了基于 IP 的认证失败限制与黑名单管理
//
// 架构：LRU 缓存（热数据，命中率高）+ bbolt（全量持久化，进程重启不丢失）。
// 这样即使在遭遇大规模爆破时，内存占用也仅与 LRUSize 相关，不会 OOM。
type AuthBlocker struct {
	db          *bbolt.DB
	lru         *lruCache
	maxFailures int
	blockDur    time.Duration
	recordTTL   time.Duration
	cleanInter  time.Duration
	stop        chan struct{}
	stopped     sync.WaitGroup
}

// NewAuthBlocker 创建并启动一个 AuthBlocker。
//
// 当 cfg.DBPath == "" 时，运行在纯内存模式（不持久化），便于单元测试。
// 生产环境应配置一个可写目录下的文件路径，例如 /var/lib/tbore/auth.db。
func NewAuthBlocker(cfg AuthBlockerConfig) (*AuthBlocker, error) {
	if cfg.MaxFailures <= 0 {
		cfg.MaxFailures = defaultMaxFailures
	}
	if cfg.BlockDuration <= 0 {
		cfg.BlockDuration = defaultBlockDuration
	}
	if cfg.LRUSize <= 0 {
		cfg.LRUSize = defaultLRUSize
	}
	if cfg.CleanInterval <= 0 {
		cfg.CleanInterval = defaultCleanInterval
	}
	if cfg.RecordTTL <= 0 {
		cfg.RecordTTL = defaultRecordTTL
	}

	b := &AuthBlocker{
		lru:         newLRUCache(cfg.LRUSize),
		maxFailures: cfg.MaxFailures,
		blockDur:    cfg.BlockDuration,
		recordTTL:   cfg.RecordTTL,
		cleanInter:  cfg.CleanInterval,
		stop:        make(chan struct{}),
	}

	if cfg.DBPath != "" {
		// 确保父目录存在，避免因目录缺失启动失败
		if dir := filepath.Dir(cfg.DBPath); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return nil, fmt.Errorf("create db directory %s: %w", dir, err)
			}
		}

		db, err := bbolt.Open(cfg.DBPath, 0o600, &bbolt.Options{
			Timeout: 5 * time.Second,
		})
		if err != nil {
			return nil, fmt.Errorf("open bbolt %s: %w", cfg.DBPath, err)
		}

		err = db.Update(func(tx *bbolt.Tx) error {
			_, err := tx.CreateBucketIfNotExists([]byte(bucketName))
			return err
		})
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("create bucket: %w", err)
		}
		b.db = db
	}

	b.stopped.Add(1)
	go b.cleanLoop()

	log.Printf("AuthBlocker started: max_failures=%d block_duration=%s lru_size=%d db_path=%q",
		b.maxFailures, b.blockDur, cfg.LRUSize, cfg.DBPath)

	return b, nil
}

// IsBlocked 检查某个 IP 当前是否被拉黑
func (b *AuthBlocker) IsBlocked(ip string) bool {
	rec := b.getRecord(ip)
	if rec == nil {
		return false
	}
	return rec.IsBlocked(time.Now())
}

// RecordFailure 记录一次认证失败，若达到阈值则更新拉黑截止时间
func (b *AuthBlocker) RecordFailure(ip string) {
	now := time.Now()
	rec := b.getRecord(ip)
	if rec == nil {
		rec = &AuthFailureRecord{}
	}

	rec.FailCount++
	rec.LastFailAt = now

	// 超过阈值即拉黑；已在拉黑窗口内则顺延截止时间
	if rec.FailCount >= b.maxFailures {
		if rec.IsBlocked(now) {
			rec.BlockedUntil = now.Add(b.blockDur)
		} else {
			rec.BlockedUntil = now.Add(b.blockDur)
		}
		log.Printf("IP %s blocked until %s after %d failures", ip, rec.BlockedUntil.Format(time.RFC3339), rec.FailCount)
	}

	b.putRecord(ip, rec)
	b.lru.put(ip, rec)
}

// ResetFailure 认证成功时调用，清除该 IP 的失败记录
func (b *AuthBlocker) ResetFailure(ip string) {
	b.deleteRecord(ip)
	b.lru.remove(ip)
}

// Close 停止后台清理并关闭数据库
func (b *AuthBlocker) Close() {
	close(b.stop)
	b.stopped.Wait()
	if b.db != nil {
		_ = b.db.Close()
	}
}

// BlockedCount 返回当前已被拉黑的 IP 数量（状态快照，仅供 status 展示）
func (b *AuthBlocker) BlockedCount() int {
	now := time.Now()
	count := 0
	b.iterateAll(func(ip string, rec *AuthFailureRecord) bool {
		if rec.IsBlocked(now) {
			count++
		}
		return true
	})
	return count
}

// cleanLoop 后台定期清理过期记录，防止 db 文件无限增长
func (b *AuthBlocker) cleanLoop() {
	defer b.stopped.Done()
	ticker := time.NewTicker(b.cleanInter)
	defer ticker.Stop()

	for {
		select {
		case <-b.stop:
			return
		case <-ticker.C:
			b.cleanExpired()
		}
	}
}

// cleanExpired 删除已过期（超过 recordTTL 未更新）的记录
//
// 注意：bbolt 不允许在 View 事务进行中再开 Update 事务，否则会死锁。
// 因此先在 View 中收集待删除 IP 列表，事务结束后再批量删除。
func (b *AuthBlocker) cleanExpired() {
	if b.db == nil {
		return
	}
	now := time.Now()
	cutoff := now.Add(-b.recordTTL)

	var toDelete []string
	b.iterateAll(func(ip string, rec *AuthFailureRecord) bool {
		// 尚在拉黑窗口内的不删
		if rec.IsBlocked(now) {
			return true
		}
		// 最后失败时间超过 TTL 的删除；零值 LastFailAt 也删
		if rec.LastFailAt.IsZero() || rec.LastFailAt.Before(cutoff) {
			toDelete = append(toDelete, ip)
		}
		return true
	})

	// 在 View 之外执行删除，避免嵌套事务死锁
	var deleted int
	for _, ip := range toDelete {
		b.deleteRecord(ip)
		b.lru.remove(ip)
		deleted++
	}

	if deleted > 0 {
		log.Printf("AuthBlocker cleaned %d expired records", deleted)
	}
}

// ===== 内部：记录读写 =====

func (b *AuthBlocker) getRecord(ip string) *AuthFailureRecord {
	// 先查内存 LRU
	if rec, ok := b.lru.get(ip); ok {
		return rec
	}
	// 再查磁盘
	if b.db == nil {
		return nil
	}
	var data []byte
	_ = b.db.View(func(tx *bbolt.Tx) error {
		bk := tx.Bucket([]byte(bucketName))
		if bk == nil {
			return nil
		}
		data = bk.Get([]byte(ip))
		return nil
	})
	if len(data) == 0 {
		return nil
	}
	var rec AuthFailureRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil
	}
	// 回填 LRU
	b.lru.put(ip, &rec)
	return &rec
}

func (b *AuthBlocker) putRecord(ip string, rec *AuthFailureRecord) {
	if b.db == nil {
		return
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return
	}
	_ = b.db.Update(func(tx *bbolt.Tx) error {
		bk, err := tx.CreateBucketIfNotExists([]byte(bucketName))
		if err != nil {
			return err
		}
		return bk.Put([]byte(ip), data)
	})
}

func (b *AuthBlocker) deleteRecord(ip string) {
	if b.db == nil {
		return
	}
	_ = b.db.Update(func(tx *bbolt.Tx) error {
		bk := tx.Bucket([]byte(bucketName))
		if bk == nil {
			return nil
		}
		return bk.Delete([]byte(ip))
	})
}

func (b *AuthBlocker) iterateAll(fn func(ip string, rec *AuthFailureRecord) bool) {
	if b.db == nil {
		return
	}
	_ = b.db.View(func(tx *bbolt.Tx) error {
		bk := tx.Bucket([]byte(bucketName))
		if bk == nil {
			return nil
		}
		c := bk.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			if len(v) == 0 {
				continue
			}
			var rec AuthFailureRecord
			if json.Unmarshal(v, &rec) != nil {
				continue
			}
			if !fn(string(k), &rec) {
				break
			}
		}
		return nil
	})
}

// ===== LRU 缓存实现 =====

type lruCache struct {
	capacity int
	mu       sync.Mutex
	items    map[string]*list.Element
	order    *list.List // 前端为最近使用
}

type lruEntry struct {
	key string
	rec *AuthFailureRecord
}

func newLRUCache(capacity int) *lruCache {
	if capacity <= 0 {
		capacity = defaultLRUSize
	}
	return &lruCache{
		capacity: capacity,
		items:    make(map[string]*list.Element),
		order:    list.New(),
	}
}

func (c *lruCache) get(key string) (*AuthFailureRecord, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.order.MoveToFront(el)
		return el.Value.(*lruEntry).rec, true
	}
	return nil, false
}

func (c *lruCache) put(key string, rec *AuthFailureRecord) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		el.Value.(*lruEntry).rec = rec
		c.order.MoveToFront(el)
		return
	}
	el := c.order.PushFront(&lruEntry{key: key, rec: rec})
	c.items[key] = el
	if c.order.Len() > c.capacity {
		oldest := c.order.Back()
		if oldest != nil {
			c.order.Remove(oldest)
			delete(c.items, oldest.Value.(*lruEntry).key)
		}
	}
}

func (c *lruCache) remove(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.order.Remove(el)
		delete(c.items, key)
	}
}

// ExtractIP 从 "host:port" 形式的 RemoteAddr 中提取 host（即 IP）
func ExtractIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}
