package redis

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/zhaochy1990/auth-service/internal/repository"
)

func newTestStore(t *testing.T) (*Store, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	return New(mr.Addr(), "", 0), mr
}

func TestReserveCooldown(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	const phone = "13812345678"

	ok, err := s.ReserveCooldown(ctx, phone)
	if err != nil || !ok {
		t.Fatalf("first ReserveCooldown = (%v, %v), want (true, nil)", ok, err)
	}
	ok, err = s.ReserveCooldown(ctx, phone)
	if err != nil || ok {
		t.Fatalf("second ReserveCooldown = (%v, %v), want (false, nil)", ok, err)
	}
}

func TestReserveDailyCount(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	const phone = "13812345678"

	for i := 0; i < 10; i++ {
		if err := s.ReserveDailyCount(ctx, phone); err != nil {
			t.Fatalf("ReserveDailyCount #%d = %v, want nil", i+1, err)
		}
	}
	if err := s.ReserveDailyCount(ctx, phone); err == nil {
		t.Fatal("11th ReserveDailyCount = nil, want daily-limit error")
	} else if err.Error() != "Daily SMS limit reached for this phone number" {
		t.Fatalf("11th ReserveDailyCount error = %q", err.Error())
	}
}

func TestReleaseSendRestoresCounters(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	const phone = "13812345678"

	if ok, err := s.ReserveCooldown(ctx, phone); err != nil || !ok {
		t.Fatalf("ReserveCooldown = (%v, %v)", ok, err)
	}
	if err := s.ReserveDailyCount(ctx, phone); err != nil {
		t.Fatalf("ReserveDailyCount = %v", err)
	}
	// A failed send must not count against the daily limit...
	if err := s.ReleaseSend(ctx, phone); err != nil {
		t.Fatalf("ReleaseSend = %v", err)
	}
	// ...and must lift the cooldown so the user can retry immediately.
	ok, err := s.ReserveCooldown(ctx, phone)
	if err != nil || !ok {
		t.Fatalf("ReserveCooldown after release = (%v, %v), want (true, nil)", ok, err)
	}
	if err := s.ReserveDailyCount(ctx, phone); err != nil {
		t.Fatalf("ReserveDailyCount after release = %v", err)
	}
}

func TestVerifyCodeLifecycle(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	const phone = "13812345678"

	// No code was ever stored.
	res, err := s.VerifyCode(ctx, phone, "123456", 5)
	if err != nil {
		t.Fatalf("VerifyCode (missing) = %v", err)
	}
	if res != repository.SmsVerifyExpired {
		t.Fatalf("VerifyCode (missing) = %d, want expired", res)
	}

	if err := s.StoreCode(ctx, phone, "123456", 5*time.Minute); err != nil {
		t.Fatalf("StoreCode = %v", err)
	}

	// Wrong code, attempts remain.
	res, err = s.VerifyCode(ctx, phone, "000000", 5)
	if err != nil {
		t.Fatalf("VerifyCode (wrong) = %v", err)
	}
	if res != repository.SmsVerifyInvalid {
		t.Fatalf("VerifyCode (wrong) = %d, want invalid", res)
	}

	// Correct code succeeds and is consumed (single-use).
	res, err = s.VerifyCode(ctx, phone, "123456", 5)
	if err != nil {
		t.Fatalf("VerifyCode (correct) = %v", err)
	}
	if res != repository.SmsVerifyOK {
		t.Fatalf("VerifyCode (correct) = %d, want ok", res)
	}
	// Replay of a consumed code is rejected.
	res, err = s.VerifyCode(ctx, phone, "123456", 5)
	if err != nil {
		t.Fatalf("VerifyCode (replay) = %v", err)
	}
	if res != repository.SmsVerifyExpired {
		t.Fatalf("VerifyCode (replay) = %d, want expired", res)
	}
}

func TestVerifyCodeAttemptCap(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	const phone = "13812345678"

	if err := s.StoreCode(ctx, phone, "123456", 5*time.Minute); err != nil {
		t.Fatalf("StoreCode = %v", err)
	}
	for i := 0; i < 4; i++ {
		res, err := s.VerifyCode(ctx, phone, "000000", 5)
		if err != nil {
			t.Fatalf("VerifyCode wrong #%d = %v", i+1, err)
		}
		if res != repository.SmsVerifyInvalid {
			t.Fatalf("VerifyCode wrong #%d = %d, want invalid", i+1, res)
		}
	}
	res, err := s.VerifyCode(ctx, phone, "000000", 5)
	if err != nil {
		t.Fatalf("VerifyCode 5th wrong = %v", err)
	}
	if res != repository.SmsVerifyAttemptsExceeded {
		t.Fatalf("VerifyCode 5th wrong = %d, want attempts-exceeded", res)
	}
	// The code was invalidated by the cap.
	res, err = s.VerifyCode(ctx, phone, "123456", 5)
	if err != nil {
		t.Fatalf("VerifyCode after cap = %v", err)
	}
	if res != repository.SmsVerifyExpired {
		t.Fatalf("VerifyCode after cap = %d, want expired", res)
	}
}

func TestStoreCodeExpiry(t *testing.T) {
	ctx := context.Background()
	s, mr := newTestStore(t)
	const phone = "13812345678"

	if err := s.StoreCode(ctx, phone, "123456", 1*time.Second); err != nil {
		t.Fatalf("StoreCode = %v", err)
	}
	mr.FastForward(2 * time.Second)
	res, err := s.VerifyCode(ctx, phone, "123456", 5)
	if err != nil {
		t.Fatalf("VerifyCode (expired) = %v", err)
	}
	if res != repository.SmsVerifyExpired {
		t.Fatalf("VerifyCode (expired) = %d, want expired", res)
	}
}

func TestStoreFailClosedOnRedisOutage(t *testing.T) {
	ctx := context.Background()
	s := New("127.0.0.1:1", "", 0) // nothing listens here
	if _, err := s.ReserveCooldown(ctx, "13812345678"); err == nil {
		t.Fatal("ReserveCooldown on unreachable Redis = nil, want fail-closed error")
	} else if !strings.Contains(err.Error(), "temporarily unavailable") {
		t.Fatalf("unexpected error: %v", err)
	}
}
