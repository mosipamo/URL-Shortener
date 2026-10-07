package store

import (
	"strconv"
	"sync/atomic"
	"testing"
)

func BenchmarkLockCompareGet(b *testing.B) {
	mu := New()
	muCode, _ := mu.Save("https://example.com/x")
	rw := NewRW()
	rwCode, _ := rw.Save("https://example.com/x")

	b.Run("Mutex", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				if _, err := mu.Get(muCode); err != nil {
					b.Fatal(err)
				}
			}
		})
	})
	b.Run("RWMutex", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				if _, err := rw.Get(rwCode); err != nil {
					b.Fatal(err)
				}
			}
		})
	})
}

func BenchmarkLockCompareSaveExisting(b *testing.B) {
	mu := New()
	if _, err := mu.Save("https://example.com/x"); err != nil {
		b.Fatal(err)
	}
	rw := NewRW()
	if _, err := rw.Save("https://example.com/x"); err != nil {
		b.Fatal(err)
	}

	b.Run("Mutex", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				if _, err := mu.Save("https://example.com/x"); err != nil {
					b.Fatal(err)
				}
			}
		})
	})
	b.Run("RWMutex", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				if _, err := rw.Save("https://example.com/x"); err != nil {
					b.Fatal(err)
				}
			}
		})
	})
}

func BenchmarkLockCompareSaveNew(b *testing.B) {
	mu := New()
	rw := NewRW()
	var muN, rwN atomic.Uint64

	b.Run("Mutex", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				u := "https://example.com/" + strconv.FormatUint(muN.Add(1), 10)
				if _, err := mu.Save(u); err != nil {
					b.Fatal(err)
				}
			}
		})
	})
	b.Run("RWMutex", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				u := "https://example.com/" + strconv.FormatUint(rwN.Add(1), 10)
				if _, err := rw.Save(u); err != nil {
					b.Fatal(err)
				}
			}
		})
	})
}

func BenchmarkLockCompareMixed(b *testing.B) {
	for _, tc := range []struct {
		name       string
		writeEvery uint64
	}{
		{"reads=90%", 10},
		{"reads=99%", 100},
		{"reads=99.9%", 1000},
	} {
		b.Run(tc.name, func(b *testing.B) {
			mu := New()
			muCode, _ := mu.Save("https://example.com/x")
			rw := NewRW()
			rwCode, _ := rw.Save("https://example.com/x")

			b.Run("Mutex", func(b *testing.B) {
				var n atomic.Uint64
				b.ReportAllocs()
				b.ResetTimer()
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						i := n.Add(1)
						if i%tc.writeEvery == 0 {
							_, _ = mu.Save("https://example.com/" + strconv.FormatUint(i, 10))
						} else {
							_, _ = mu.Get(muCode)
						}
					}
				})
			})
			b.Run("RWMutex", func(b *testing.B) {
				var n atomic.Uint64
				b.ReportAllocs()
				b.ResetTimer()
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						i := n.Add(1)
						if i%tc.writeEvery == 0 {
							_, _ = rw.Save("https://example.com/" + strconv.FormatUint(i, 10))
						} else {
							_, _ = rw.Get(rwCode)
						}
					}
				})
			})
		})
	}
}
