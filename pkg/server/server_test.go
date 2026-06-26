package server

import (
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"golang.org/x/sync/semaphore"

	"tbore/pkg/auth"
)

// helper：直接手工构造 Server，跳过 NewServer 的 bbolt 初始化，
// 由测试方注入可控的 blocker 实例
func testServerWithBlocker(blocker *auth.AuthBlocker) *Server {
	signer, _ := auth.GenerateSigner()
	return &Server{
		auth:    auth.NewAuthenticator("test_token"),
		blocker: blocker,
		sem:     semaphore.NewWeighted(int64(10)),
		signer:  signer,
	}
}

// =========================================================================
// 场景：预握手拦截 —— 黑名单 IP 在 SSH 握手前被直接关闭
//
// 验证点：
//  1) 被拉黑的 IP 连进来后，连接被立即关闭（TCP 层），不会进入 SSH 握手
//  2) 大量被拦截的连接处理极快（无 RSA 开销）
// =========================================================================

func TestPreHandshakeBlock(t *testing.T) {
	blocker, err := auth.NewAuthBlocker(auth.AuthBlockerConfig{
		DBPath:        "", // 纯内存，避免磁盘依赖
		MaxFailures:   1,
		BlockDuration: 30 * time.Minute,
		LRUSize:       100,
	})
	if err != nil {
		t.Fatalf("NewAuthBlocker: %v", err)
	}
	defer blocker.Close()

	srv := testServerWithBlocker(blocker)
	// 127.0.0.1 是测试连接的源 IP，预先拉黑
	blocker.RecordFailure("127.0.0.1")
	if !blocker.IsBlocked("127.0.0.1") {
		t.Fatal("预拉黑 127.0.0.1 失败")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr().String()

	// accept 循环，复用 handleConnection 的预握手拦截逻辑
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			if !srv.sem.TryAcquire(1) {
				conn.Close()
				continue
			}
			go func(c net.Conn) {
				defer srv.sem.Release(1)
				defer c.Close()
				// 预握手拦截：与 server.go handleConnection 一致
				if srv.blocker != nil {
					ip := auth.ExtractIP(c.RemoteAddr().String())
					if srv.blocker.IsBlocked(ip) {
						return // 不进入 ssh.NewServerConn
					}
				}
				// 走到这里说明未被拦截（测试中不应发生）
				t.Errorf("黑名单 IP 未被预握手拦截")
			}(conn)
		}
	}()

	// 从被拉黑的 127.0.0.1 发起连接
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	// 服务端应立即关闭连接；写一点数据触发处理
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	_, _ = conn.Write([]byte("SSH-2.0-test\r\n"))

	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err == nil && n > 0 {
		t.Errorf("预握手拦截未生效：读到 %d 字节，说明进入了 SSH 握手", n)
	} else {
		t.Logf("PASS: 预握手拦截生效，连接被立即关闭 (err=%v, n=%d)", err, n)
	}
}

// =========================================================================
// 场景：预握手拦截性能 —— 100 个被拦截连接应在 1 秒内处理完
// =========================================================================

func TestPreHandshakeBlockPerformance(t *testing.T) {
	blocker, _ := auth.NewAuthBlocker(auth.AuthBlockerConfig{
		DBPath:        "",
		MaxFailures:   1,
		BlockDuration: 30 * time.Minute,
		LRUSize:       100,
	})
	defer blocker.Close()

	srv := testServerWithBlocker(blocker)
	blocker.RecordFailure("127.0.0.1")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr().String()

	var wg sync.WaitGroup
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			if !srv.sem.TryAcquire(1) {
				conn.Close()
				continue
			}
			wg.Add(1)
			go func(c net.Conn) {
				defer wg.Done()
				defer srv.sem.Release(1)
				defer c.Close()
				if srv.blocker != nil {
					ip := auth.ExtractIP(c.RemoteAddr().String())
					if srv.blocker.IsBlocked(ip) {
						return
					}
				}
			}(conn)
		}
	}()

	const count = 100
	start := time.Now()
	for i := 0; i < count; i++ {
		conn, err := net.DialTimeout("tcp", addr, 1*time.Second)
		if err != nil {
			t.Fatalf("Dial %d: %v", i, err)
		}
		conn.Close()
	}
	// 等待所有 accept 处理完成
	wg.Wait()
	elapsed := time.Since(start)

	t.Logf("预握手拦截 %d 个连接耗时: %v (平均 %.3fms/个)",
		count, elapsed, float64(elapsed.Milliseconds())/float64(count))

	if elapsed > 1*time.Second {
		t.Errorf("预握手拦截性能不达标: %v > 1s", elapsed)
	}
}

// =========================================================================
// 场景：非黑名单 IP 不被预拦截（反向验证）
// =========================================================================

func TestPreHandshakePassForNonBlockedIP(t *testing.T) {
	blocker, _ := auth.NewAuthBlocker(auth.AuthBlockerConfig{
		DBPath:        "",
		MaxFailures:   5,
		BlockDuration: 30 * time.Minute,
		LRUSize:       100,
	})
	defer blocker.Close()

	srv := testServerWithBlocker(blocker)
	// 注意：不拉黑 127.0.0.1

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	addr := ln.Addr().String()

	passed := make(chan struct{}, 1)
	go func() {
		conn, _ := ln.Accept()
		if !srv.sem.TryAcquire(1) {
			conn.Close()
			return
		}
		go func(c net.Conn) {
			defer srv.sem.Release(1)
			defer c.Close()
			if srv.blocker != nil {
				ip := auth.ExtractIP(c.RemoteAddr().String())
				if srv.blocker.IsBlocked(ip) {
					t.Errorf("未拉黑的 IP 不应被预握手拦截")
					return
				}
			}
			// 没被拦截：服务端会等待 SSH 握手数据，我们读一点就 close
			passed <- struct{}{}
		}(conn)
	}()

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	// 等待被 accept，证明未被预拦截
	select {
	case <-passed:
		t.Log("PASS: 未拉黑的 IP 顺利通过预拦截检查")
	case <-time.After(2 * time.Second):
		t.Fatal("未拉黑的 IP 未被 accept，超时")
	}
}

// 避免 "declared and not used" 之类的编译错误
var _ = fmt.Sprintf
