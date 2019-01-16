package ssb

import (
	"log"
	"net"
	"sync"
	"time"

	"github.com/go-kit/kit/metrics"
	"go.cryptoscope.co/netwrap"
	"go.cryptoscope.co/secretstream"
)

type instrumentedConnTracker struct {
	root ConnTracker

	count     metrics.Gauge
	durration metrics.Histogram
}

func NewInstrumentedConnTracker(r ConnTracker, ct metrics.Gauge, h metrics.Histogram) ConnTracker {
	i := instrumentedConnTracker{root: r, count: ct, durration: h}
	return &i
}

func (ict instrumentedConnTracker) Count() uint {
	n := ict.root.Count()
	ict.count.With("part", "tracked_count").Set(float64(n))
	return n
}

func (ict instrumentedConnTracker) CloseAll() {
	ict.root.CloseAll()
}

func (ict instrumentedConnTracker) OnAccept(conn net.Conn) bool {
	ok := ict.root.OnAccept(conn)
	if ok {
		ict.count.With("part", "tracked_conns").Add(1)
	}
	return ok
}

func (ict instrumentedConnTracker) OnClose(conn net.Conn) time.Duration {
	durr := ict.root.OnClose(conn)
	if durr > 0 {
		ict.count.With("part", "tracked_conns").Add(-1)
		ict.durration.With("part", "tracked_conns").Observe(durr.Seconds())
	}
	return durr
}

type ConnTracker interface {
	OnAccept(conn net.Conn) bool
	OnClose(conn net.Conn) time.Duration
	Count() uint
	CloseAll()
}
type connEntry struct {
	c       net.Conn
	started time.Time
}
type connLookupMap map[[32]byte]connEntry

func NewConnTracker() ConnTracker {
	return &connTracker{active: make(connLookupMap)}
}

// tracks open connections and refuses to established pubkeys
type connTracker struct {
	activeLock sync.Mutex
	active     connLookupMap
}

func (ct *connTracker) CloseAll() {
	ct.activeLock.Lock()
	defer ct.activeLock.Unlock()
	for k, c := range ct.active {
		if err := c.c.Close(); err != nil {
			log.Printf("failed to close %x: %v\n", k[:5], err)
		}
	}
}

func (ct *connTracker) Count() uint {
	ct.activeLock.Lock()
	defer ct.activeLock.Unlock()
	return uint(len(ct.active))
}

func toActive(a net.Addr) [32]byte {
	var pk [32]byte
	shs, ok := netwrap.GetAddr(a, "shs-bs").(secretstream.Addr)
	if ok {
		copy(pk[:], shs.PubKey)
	}
	return pk
}

func (ct *connTracker) OnAccept(conn net.Conn) bool {
	ct.activeLock.Lock()
	defer ct.activeLock.Unlock()
	k := toActive(conn.RemoteAddr())
	_, ok := ct.active[k]
	if ok {
		return false
	}
	ct.active[k] = connEntry{
		c:       conn,
		started: time.Now(),
	}
	return true
}

func (ct *connTracker) OnClose(conn net.Conn) time.Duration {
	ct.activeLock.Lock()
	defer ct.activeLock.Unlock()

	k := toActive(conn.RemoteAddr())
	who, ok := ct.active[k]
	if !ok {
		return 0
	}
	delete(ct.active, k)
	return time.Since(who.started)
}
