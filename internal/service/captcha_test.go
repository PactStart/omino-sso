package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image/png"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// fakeCaptchaStore 用内存 map 模拟 Redis，验证 TTL、一次性消费与错误传播。
type fakeCaptchaStore struct {
	values map[string]string
	ttls   map[string]time.Duration
	setErr error
}

func newFakeCaptchaStore() *fakeCaptchaStore {
	return &fakeCaptchaStore{
		values: map[string]string{},
		ttls:   map[string]time.Duration{},
	}
}

func (f *fakeCaptchaStore) Set(_ context.Context, key string, value interface{}, ttl time.Duration) *redis.StatusCmd {
	if f.setErr != nil {
		return redis.NewStatusResult("", f.setErr)
	}
	f.values[key] = fmt.Sprint(value)
	f.ttls[key] = ttl
	return redis.NewStatusResult("OK", nil)
}

func (f *fakeCaptchaStore) GetDel(_ context.Context, key string) *redis.StringCmd {
	value, ok := f.values[key]
	if !ok {
		return redis.NewStringResult("", redis.Nil)
	}
	delete(f.values, key)
	return redis.NewStringResult(value, nil)
}

func newTestCaptcha(t *testing.T) (*CaptchaService, *fakeCaptchaStore) {
	t.Helper()
	store := newFakeCaptchaStore()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return newCaptchaService(store, logger), store
}

func TestCaptchaIssueStoresCode(t *testing.T) {
	svc, store := newTestCaptcha(t)

	cid, image, err := svc.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue 失败: %v", err)
	}
	if len(cid) != 26 {
		t.Errorf("captchaId 长度应为 26，实际 %d（%q）", len(cid), cid)
	}
	if !strings.HasPrefix(image, "data:image/png;base64,") {
		t.Errorf("image 应为 PNG data URL，实际前缀 %q", image[:min(32, len(image))])
	}

	code, ok := store.values[captchaKeyPrefix+cid]
	if !ok {
		t.Fatal("未写入验证码到存储")
	}
	if len(code) != captchaLength {
		t.Errorf("验证码长度应为 %d，实际 %d", captchaLength, len(code))
	}
	for _, ch := range code {
		if !strings.ContainsRune(captchaAlphabet, ch) {
			t.Errorf("验证码字符 %q 不在字母表内", ch)
		}
	}
	if ttl := store.ttls[captchaKeyPrefix+cid]; ttl != CaptchaTTLSeconds*time.Second {
		t.Errorf("TTL 应为 %s，实际 %s", CaptchaTTLSeconds*time.Second, ttl)
	}
}

// TestCaptchaImageCarriesNoPlaintext 守住「答案不得随图明文下发」这条线：
// 图片必须是可解码的 PNG 位图，且原始字节里不能出现验证码字符串。
func TestCaptchaImageCarriesNoPlaintext(t *testing.T) {
	svc, _ := newTestCaptcha(t)

	cid, imageURL, err := svc.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue 失败: %v", err)
	}
	store := svc.st.(*fakeCaptchaStore)
	code := store.values[captchaKeyPrefix+cid]
	if code == "" {
		t.Fatal("未取到已签发的验证码")
	}

	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(imageURL, "data:image/png;base64,"))
	if err != nil {
		t.Fatalf("data URL 不是合法 base64: %v", err)
	}
	if bytes.Contains(raw, []byte(code)) {
		t.Fatalf("PNG 字节中出现验证码明文 %q，脚本可直接解码", code)
	}

	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("图片不是合法 PNG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != captchaWidth || b.Dy() != captchaHeight {
		t.Fatalf("图片尺寸应为 %dx%d，实际 %dx%d", captchaWidth, captchaHeight, b.Dx(), b.Dy())
	}

	// 至少有深色像素，排除「白图」导致人眼不可读。
	ink := 0
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			r, _, _, _ := img.At(x, y).RGBA()
			if r>>8 < 0x80 {
				ink++
			}
		}
	}
	if ink < 60 {
		t.Fatalf("深色像素仅 %d 个，字符可能未绘制", ink)
	}

	// 4 个字符都必须落在画布内：历史实现把第 4 个字符画到 x=121（画布仅 120 宽），
	// 页面上只能看到 3 个字符，这里守住最右字符区域必须有笔画。
	origins := glyphOrigins(captchaLength, captchaMargin, 7*captchaScale, captchaWidth)
	rightInk := 0
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := origins[len(origins)-1] - 8; x < captchaWidth; x++ {
			r, _, _, _ := img.At(x, y).RGBA()
			if r>>8 < 0x80 {
				rightInk++
			}
		}
	}
	if rightInk < 20 {
		t.Fatalf("最右字符区域深色像素仅 %d 个，字符可能被画出画布", rightInk)
	}
}

func TestCaptchaGlyphLayoutFitsCanvas(t *testing.T) {
	glyphWidth := 7 * captchaScale
	// 只覆盖实际可能出现的字符数（captchaLength=4，及其更少的情形）。
	for _, count := range []int{1, 2, 3, captchaLength} {
		origins := glyphOrigins(count, captchaMargin, glyphWidth, captchaWidth)
		if len(origins) != count {
			t.Fatalf("count=%d 时返回 %d 个起点", count, len(origins))
		}
		for i, x := range origins {
			if x < captchaMargin {
				t.Errorf("count=%d 第 %d 个起点 %d 越过左留白 %d", count, i, x, captchaMargin)
			}
			if x+glyphWidth > captchaWidth-captchaMargin {
				t.Errorf("count=%d 第 %d 个字符右边界 %d 越过画布可用右界 %d",
					count, i, x+glyphWidth, captchaWidth-captchaMargin)
			}
			if i > 0 && x <= origins[i-1] {
				t.Errorf("count=%d 起点未递增：%v", count, origins)
			}
		}
	}
}

func TestCaptchaVerifyConsumesOnce(t *testing.T) {
	svc, store := newTestCaptcha(t)
	ctx := context.Background()

	cid, _, err := svc.Issue(ctx)
	if err != nil {
		t.Fatalf("Issue 失败: %v", err)
	}
	code := store.values[captchaKeyPrefix+cid]

	if err := svc.Verify(ctx, cid, strings.ToLower(code)); err != nil {
		t.Fatalf("大小写不敏感的验证码应通过: %v", err)
	}
	// 一次性消费：同一验证码再次校验必须失败。
	if err := svc.Verify(ctx, cid, code); !errors.Is(err, ErrCaptchaInvalid) {
		t.Fatalf("重复使用应返回 ErrCaptchaInvalid，实际 %v", err)
	}
}

func TestCaptchaVerifyRejectsWrongOrMissing(t *testing.T) {
	svc, _ := newTestCaptcha(t)
	ctx := context.Background()

	cid, _, err := svc.Issue(ctx)
	if err != nil {
		t.Fatalf("Issue 失败: %v", err)
	}

	cases := []struct {
		name string
		id   string
		code string
	}{
		{"错误验证码", cid, "ZZZZ"},
		{"缺少 id", "", "ABCD"},
		{"缺少验证码", cid, ""},
		{"未知 id", "01J9ZK2Q3W5X7Y9A1B3C5D7E9F", "ABCD"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := svc.Verify(ctx, c.id, c.code); !errors.Is(err, ErrCaptchaInvalid) {
				t.Fatalf("应返回 ErrCaptchaInvalid，实际 %v", err)
			}
		})
	}
}

func TestCaptchaIssuePropagatesStoreError(t *testing.T) {
	svc, store := newTestCaptcha(t)
	store.setErr = errors.New("redis down")

	if _, _, err := svc.Issue(context.Background()); err == nil {
		t.Fatal("存储失败时 Issue 应返回错误")
	}
}
