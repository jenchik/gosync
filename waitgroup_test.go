// Based on https://github.com/golang/go/blob/master/src/sync/waitgroup_test.go
//
// Changed tests:
// Now no skipping known-racy test under the race detector
// TestWaitGroupMisuse2 => TestWaitGroupNoMisuse2
// TestWaitGroupMisuse3 => TestWaitGroupNoMisuse3

package gosync_test

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	. "github.com/jenchik/gosync"
)

func testWaitGroup(t *testing.T, wg1 WaitGroup, wg2 WaitGroup) {
	n := 16
	wg1.Add(n)
	wg2.Add(n)
	exited := make(chan bool, n)
	for i := 0; i != n; i++ {
		go func() {
			wg1.Done()
			wg2.Wait()
			exited <- true
		}()
	}
	wg1.Wait()
	for i := 0; i != n; i++ {
		select {
		case <-exited:
			t.Fatal("WaitGroup released group too soon")
		default:
		}
		wg2.Done()
	}
	for i := 0; i != n; i++ {
		<-exited // Will block if barrier fails to unlock someone.
	}
}

func TestWaitGroup(t *testing.T) {
	wg1 := NewWaitGroup()
	wg2 := NewWaitGroup()

	// Run the same test a few times to ensure barrier is in a proper state.
	for i := 0; i != 8; i++ {
		testWaitGroup(t, wg1, wg2)
	}
}

func TestWaitGroupMisuse(t *testing.T) {
	defer func() {
		err := recover()
		if err != "waitgroup: negative WaitGroup counter" {
			t.Fatalf("Unexpected panic: %#v", err)
		}
	}()
	wg := NewWaitGroup()
	wg.Add(1)
	wg.Done()
	wg.Done()
	t.Fatal("Should panic")
}

func TestWaitGroupNoMisuse2(t *testing.T) {
	if runtime.NumCPU() <= 4 {
		t.Skip("NumCPU<=4, skipping: this test requires parallelism")
	}
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(4))
	done := make(chan interface{}, 2)
	for i := 0; i < 1000; i++ {
		wg := NewWaitGroup()
		var here uint32
		wg.Add(1)
		go func() {
			defer func() {
				done <- recover()
			}()
			atomic.AddUint32(&here, 1)
			for atomic.LoadUint32(&here) != 3 {
				// spin
			}
			wg.Wait()
		}()
		go func() {
			defer func() {
				done <- recover()
			}()
			atomic.AddUint32(&here, 1)
			for atomic.LoadUint32(&here) != 3 {
				// spin
			}
			wg.Add(1) // This is the bad guy.
			wg.Done()
		}()
		atomic.AddUint32(&here, 1)
		for atomic.LoadUint32(&here) != 3 {
			// spin
		}
		wg.Done()
		for j := 0; j < 2; j++ {
			if err := <-done; err != nil {
				panic(err)
			}
		}
	}
}

func TestWaitGroupNoMisuse3(t *testing.T) {
	if runtime.NumCPU() <= 1 {
		t.Skip("NumCPU==1, skipping: this test requires parallelism")
	}
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(4))
	done := make(chan interface{}, 2)
	for i := 0; i < 1000; i++ {
		wg := NewWaitGroup()
		wg.Add(1)
		go func() {
			defer func() {
				done <- recover()
			}()
			wg.Done()
		}()
		go func() {
			defer func() {
				done <- recover()
			}()
			wg.Wait()
			// Start reusing the wg before waiting for the Wait below to return.
			wg.Add(1)
			go func() {
				wg.Done()
			}()
			wg.Wait()
		}()
		wg.Wait()
		for j := 0; j < 2; j++ {
			if err := <-done; err != nil {
				panic(err)
			}
		}
	}
}

func TestWaitGroupRace(t *testing.T) {
	// Run this test for about 1ms.
	for i := 0; i < 1000; i++ {
		wg := NewWaitGroup()
		n := new(int32)
		// spawn goroutine 1
		wg.Add(1)
		go func() {
			atomic.AddInt32(n, 1)
			wg.Done()
		}()
		// spawn goroutine 2
		wg.Add(1)
		go func() {
			atomic.AddInt32(n, 1)
			wg.Done()
		}()
		// Wait for goroutine 1 and 2
		wg.Wait()
		if atomic.LoadInt32(n) != 2 {
			t.Fatal("Spurious wakeup from Wait")
		}
	}
}

func TestWaitGroupAlign(t *testing.T) {
	type X struct {
		x  byte
		wg WaitGroup
	}
	var x X
	x.wg = NewWaitGroup()
	x.wg.Add(1)
	go func(x *X) {
		x.wg.Done()
	}(&x)
	x.wg.Wait()
}

func BenchmarkWaitGroupUncontended(b *testing.B) {
	type PaddedWaitGroup struct {
		WaitGroup
		pad [128]uint8
	}
	b.RunParallel(func(pb *testing.PB) {
		wg := PaddedWaitGroup{
			WaitGroup: NewWaitGroup(),
		}
		for pb.Next() {
			wg.Add(1)
			wg.Done()
			wg.Wait()
		}
	})
}

func BenchmarkWaitGroupUncontended_sys(b *testing.B) {
	type PaddedWaitGroup struct {
		sync.WaitGroup
		pad [128]uint8
	}
	b.RunParallel(func(pb *testing.PB) {
		var wg PaddedWaitGroup
		for pb.Next() {
			wg.Add(1)
			wg.Done()
			wg.Wait()
		}
	})
}

func benchmarkWaitGroupAddDone(b *testing.B, wg WaitGroup, localWork int) {
	b.RunParallel(func(pb *testing.PB) {
		foo := 0
		for pb.Next() {
			wg.Add(1)
			for i := 0; i < localWork; i++ {
				foo *= 2
				foo /= 2
			}
			wg.Done()
		}
		_ = foo
	})
}

func BenchmarkWaitGroupAddDone(b *testing.B) {
	benchmarkWaitGroupAddDone(b, NewWaitGroup(), 0)
}

func BenchmarkWaitGroupAddDone_sys(b *testing.B) {
	wg := new(sync.WaitGroup)
	benchmarkWaitGroupAddDone(b, wg, 0)
}

func BenchmarkWaitGroupAddDoneWork(b *testing.B) {
	benchmarkWaitGroupAddDone(b, NewWaitGroup(), 100)
}

func BenchmarkWaitGroupAddDoneWork_sys(b *testing.B) {
	wg := new(sync.WaitGroup)
	benchmarkWaitGroupAddDone(b, wg, 100)
}

func benchmarkWaitGroupWait(b *testing.B, wg WaitGroup, localWork int) {
	b.RunParallel(func(pb *testing.PB) {
		foo := 0
		for pb.Next() {
			wg.Wait()
			for i := 0; i < localWork; i++ {
				foo *= 2
				foo /= 2
			}
		}
		_ = foo
	})
}

func BenchmarkWaitGroupWait(b *testing.B) {
	benchmarkWaitGroupWait(b, NewWaitGroup(), 0)
}

func BenchmarkWaitGroupWait_sys(b *testing.B) {
	wg := new(sync.WaitGroup)
	benchmarkWaitGroupWait(b, wg, 0)
}

func BenchmarkWaitGroupWaitWork(b *testing.B) {
	benchmarkWaitGroupWait(b, NewWaitGroup(), 100)
}

func BenchmarkWaitGroupWaitWork_sys(b *testing.B) {
	wg := new(sync.WaitGroup)
	benchmarkWaitGroupWait(b, wg, 100)
}

func BenchmarkWaitGroupActuallyWait(b *testing.B) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			wg := NewWaitGroup()
			wg.Add(1)
			go func() {
				wg.Done()
			}()
			wg.Wait()
		}
	})
}

func BenchmarkWaitGroupActuallyWait_sys(b *testing.B) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				wg.Done()
			}()
			wg.Wait()
		}
	})
}
