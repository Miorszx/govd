package util

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func cookieBody(value string) string {
	return "# Netscape HTTP Cookie File\n" +
		".facebook.com\tTRUE\t/\tTRUE\t9999999999\tc_user\t" + value + "\n"
}

func writeCookie(t *testing.T, dir, name, value string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(cookieBody(value)), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// Cache mesti pulangkan kandungan fail TERKINI. Kalau operator export semula
// cookie (tulis fail baru), pembacaan seterusnya mesti nampak nilai baru
// tanpa restart proses.
func TestCacheInvalidatesOnRewrite(t *testing.T) {
	dir := t.TempDir()
	name := "rewrite.txt"

	writeCookie(t, dir, name, "OLD")
	first := parseCookieFileFrom(dir, name)
	if len(first) != 1 || first[0].Value != "OLD" {
		t.Fatalf("first read: got %d cookies", len(first))
	}

	// tulis semula dengan nilai berbeza dan mtime berbeza
	writeCookie(t, dir, name, "NEWER")
	future := time.Now().Add(3 * time.Second)
	os.Chtimes(filepath.Join(dir, name), future, future)

	second := parseCookieFileFrom(dir, name)
	if len(second) != 1 || second[0].Value != "NEWER" {
		t.Fatalf("STALE CACHE: expected NEWER after rewrite, got %+v", second)
	}
}

// Panggilan serentak mesti selamat. Dijalankan dengan -race untuk mengesan
// map read/write tanpa lock.
func TestCacheConcurrentAccess(t *testing.T) {
	dir := t.TempDir()

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("concurrent_%d.txt", i)
		writeCookie(t, dir, name, fmt.Sprintf("VAL%d", i))
	}

	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("concurrent_%d.txt", i%5)
			got := parseCookieFileFrom(dir, name)
			if len(got) != 1 {
				t.Errorf("goroutine %d: expected 1 cookie, got %d", i, len(got))
			}
		}(i)
	}
	wg.Wait()
}

// Fail yang belum wujud tidak boleh meracuni cache: bila fail muncul
// kemudian, pembacaan mesti berjaya.
func TestCacheMissingFileDoesNotPoison(t *testing.T) {
	dir := t.TempDir()
	name := "late.txt"

	if got := parseCookieFileFrom(dir, name); got != nil {
		t.Fatalf("expected nil for missing file, got %d", len(got))
	}

	writeCookie(t, dir, name, "LATE")
	got := parseCookieFileFrom(dir, name)
	if len(got) != 1 || got[0].Value != "LATE" {
		t.Fatalf("cache poisoned by earlier miss: got %+v", got)
	}
}

// Pemanggil tidak boleh mengubah isi cache. Kalau pemanggil ubah slice/nilai
// yang dipulangkan, pembacaan seterusnya mesti masih bersih.
func TestReturnedCookiesAreIsolated(t *testing.T) {
	dir := t.TempDir()
	name := "isolated.txt"
	writeCookie(t, dir, name, "PRISTINE")

	first := parseCookieFileFrom(dir, name)
	if len(first) != 1 {
		t.Fatalf("expected 1 cookie")
	}
	first[0].Value = "TAMPERED"
	first[0].Name = "hacked"

	second := parseCookieFileFrom(dir, name)
	if len(second) != 1 {
		t.Fatalf("expected 1 cookie")
	}
	if second[0].Value != "PRISTINE" || second[0].Name != "c_user" {
		t.Fatalf("CACHE MUTATED by caller: %+v", second[0])
	}
}

// Nama fail kosong mesti pulangkan nil tanpa panic.
func TestEmptyFileNameReturnsNil(t *testing.T) {
	if got := parseCookieFileFrom(t.TempDir(), ""); got != nil {
		t.Fatalf("expected nil, got %d", len(got))
	}
}

// Fail cookie yang rosak mesti pulangkan nil, TIDAK panic. Ini juga menutup
// laluan logger.Warnf yang hanya dicapai bila parse gagal (logger mungkin
// belum diinisialisasi dalam test / proses awal).
func TestCorruptCookieFileDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	name := "corrupt.txt"
	if err := os.WriteFile(filepath.Join(dir, name),
		[]byte("this is not a netscape cookie file at all\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("PANIC on corrupt cookie file: %v", r)
		}
	}()

	if got := parseCookieFileFrom(dir, name); got != nil {
		t.Fatalf("expected nil for corrupt file, got %d cookies", len(got))
	}
}
