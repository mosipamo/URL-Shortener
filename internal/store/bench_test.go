package store

import (
	"strconv"
	"sync/atomic"
	"testing"
)

func BenchmarkMemoryGet(b *testing.B) {
	s := New()
	code, _ := s.Save("https://example.com/x")
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := s.Get(code); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkMemorySaveExisting(b *testing.B) {
	s := New()
	if _, err := s.Save("https://example.com/x"); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := s.Save("https://example.com/x"); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkMemorySaveNew(b *testing.B) {
	s := New()
	var n atomic.Uint64
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			u := "https://example.com/" + strconv.FormatUint(n.Add(1), 10)
			if _, err := s.Save(u); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkMemoryMixed(b *testing.B) {
	for _, tc := range []struct {
		name       string
		writeEvery uint64
	}{
		{"reads=90%", 10},
		{"reads=99%", 100},
		{"reads=99.9%", 1000},
	} {
		b.Run(tc.name, func(b *testing.B) {
			s := New()
			code, _ := s.Save("https://example.com/x")
			var n atomic.Uint64
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					i := n.Add(1)
					if i%tc.writeEvery == 0 {
						_, _ = s.Save("https://example.com/" + strconv.FormatUint(i, 10))
					} else {
						_, _ = s.Get(code)
					}
				}
			})
		})
	}
}

func BenchmarkGormGet(b *testing.B) {
	s := newBenchGorm(b)
	code, _ := s.Save("https://example.com/x")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := s.Get(code); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGormSaveExisting(b *testing.B) {
	s := newBenchGorm(b)
	if _, err := s.Save("https://example.com/x"); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := s.Save("https://example.com/x"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGormSaveNew(b *testing.B) {
	s := newBenchGorm(b)
	var n atomic.Uint64
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		u := "https://example.com/" + strconv.FormatUint(n.Add(1), 10)
		if _, err := s.Save(u); err != nil {
			b.Fatal(err)
		}
	}
}
