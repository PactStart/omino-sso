package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log/slog"
	"math"
	"math/big"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"github.com/omino/omino-sso/internal/pkg/id"
	"github.com/omino/omino-sso/internal/store"
)

const (
	// captchaKeyPrefix 图形验证码 Redis 键前缀（与验证码短串分开，互不影响）。
	captchaKeyPrefix = "captcha:"
	// CaptchaTTLSeconds 图形验证码有效期（秒），与 CaptchaService 的 TTL 保持一致。
	CaptchaTTLSeconds = 120
	// captchaAlphabet 去掉易混淆字符：0/O、1/I/L、2/Z，以及位图字体下与 8/5 难分的 B/S。
	captchaAlphabet = "3456789ACDEFGHJKMNPQRTUVWXY"
	captchaLength   = 4
)

// ErrCaptchaInvalid 图形验证码缺失、错误、过期或已使用。
var ErrCaptchaInvalid = errors.New("captcha invalid")

// captchaStore 图形验证码所需的最小命令集：抽取接口以便用假实现做单测。
type captchaStore interface {
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd
	GetDel(ctx context.Context, key string) *redis.StringCmd
}

// CaptchaService 图形验证码：Redis 短时存储 + 一次性消费，用于未登录接口防刷。
// 图片以 PNG 位图下发——绝不能把字符作为文本节点/明文随图返回（否则脚本解码即得答案）。
type CaptchaService struct {
	st  captchaStore
	log *slog.Logger
	ttl time.Duration
}

// NewCaptchaService 构造图形验证码服务。
func NewCaptchaService(st *store.Store, logger *slog.Logger) *CaptchaService {
	return newCaptchaService(st.Redis(), logger)
}

func newCaptchaService(st captchaStore, logger *slog.Logger) *CaptchaService {
	if logger == nil {
		logger = slog.Default()
	}
	return &CaptchaService{st: st, log: logger, ttl: CaptchaTTLSeconds * time.Second}
}

// Issue 生成 4 位图形验证码，返回验证码 id 与可直接作为 <img src> 的 PNG data URL。
func (s *CaptchaService) Issue(ctx context.Context) (string, string, error) {
	code, err := randomCaptcha()
	if err != nil {
		return "", "", err
	}
	cid := id.New()
	if err := s.st.Set(ctx, captchaKeyPrefix+cid, code, s.ttl).Err(); err != nil {
		return "", "", fmt.Errorf("store captcha: %w", err)
	}
	imageURL, err := captchaPNG(code)
	if err != nil {
		return "", "", err
	}
	return cid, imageURL, nil
}

// Verify 校验并消费图形验证码；缺失、错误、过期或重复使用均返回 ErrCaptchaInvalid。
func (s *CaptchaService) Verify(ctx context.Context, captchaID, code string) error {
	captchaID = strings.TrimSpace(captchaID)
	code = strings.TrimSpace(code)
	if captchaID == "" || code == "" {
		return ErrCaptchaInvalid
	}
	// 一次性消费：GETDEL 原子取出并删除，避免同一图形码被并发复用。
	stored, err := s.st.GetDel(ctx, captchaKeyPrefix+captchaID).Result()
	if errors.Is(err, redis.Nil) {
		return ErrCaptchaInvalid
	}
	if err != nil {
		return fmt.Errorf("load captcha: %w", err)
	}
	if !strings.EqualFold(stored, code) {
		return ErrCaptchaInvalid
	}
	return nil
}

// randomCaptcha 从去混淆字母表中等概率取 4 位。
func randomCaptcha() (string, error) {
	buf := make([]byte, captchaLength)
	limit := big.NewInt(int64(len(captchaAlphabet)))
	for i := range buf {
		n, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", err
		}
		buf[i] = captchaAlphabet[n.Int64()]
	}
	return string(buf), nil
}

const (
	captchaWidth  = 132
	captchaHeight = 44
	// captchaScale 字符放大倍数：7x13 位图字体放大后约 21x39 像素，保证 120+ 宽度下笔画清晰。
	captchaScale = 3
	// captchaMargin 左右留白（像素），同时给剪切位移余量，避免字符贴边被裁掉。
	captchaMargin = 8
)

// glyphOrigins 计算每个字符的起始 x：在画布内等距分布，且保证最后一个字符完整落在
// [margin, width-margin] 内。历史实现用「剩余宽度 / 3」当字符步长，导致第 4 个字符
// 被画到画布外（只剩 3 个字符可见），这里改为按真实字宽反推间隙。
func glyphOrigins(count, margin, glyphWidth, canvasWidth int) []int {
	origins := make([]int, count)
	if count <= 0 {
		return origins
	}
	gap := 0
	if count > 1 {
		if usable := canvasWidth - 2*margin - count*glyphWidth; usable > 0 {
			gap = usable / (count - 1)
		}
	}
	for i := range origins {
		origins[i] = margin + i*(glyphWidth+gap)
	}
	return origins
}

// captchaPNG 把验证码渲染为 PNG data URL。
// 字符只以像素形式存在（位图字体绘制 + 剪切 + 干扰线 + 噪点），
// 响应中不出现任何可直接解析出答案的文本节点，脚本必须走 OCR 才能识别。
func captchaPNG(code string) (string, error) {
	img := image.NewRGBA(image.Rect(0, 0, captchaWidth, captchaHeight))
	bg := color.RGBA{R: 0xF8, G: 0xFA, B: 0xFC, A: 0xFF}
	draw.Draw(img, img.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)

	// 干扰线：先画在字符下方，避免遮挡笔画。
	for i := 0; i < 3; i++ {
		x1, y1 := randomInt(captchaWidth), randomInt(captchaHeight)
		x2, y2 := randomInt(captchaWidth), randomInt(captchaHeight)
		drawLine(img, x1, y1, x2, y2, color.RGBA{R: 0xCB, G: 0xD5, B: 0xE1, A: 0xFF})
	}

	ink := color.RGBA{R: 0x1E, G: 0x29, B: 0x3B, A: 0xFF}
	glyphWidth := 7 * captchaScale
	origins := glyphOrigins(len([]rune(code)), captchaMargin, glyphWidth, captchaWidth)
	for i, ch := range code {
		slot := image.NewRGBA(image.Rect(0, 0, 7, 13))
		(&font.Drawer{
			Dst:  slot,
			Src:  image.NewUniform(ink),
			Face: basicfont.Face7x13,
			Dot:  fixed.P(0, 11),
		}).DrawString(string(ch))
		glyph := scaleNearest(slot, captchaScale)
		slope := (float64(randomInt(9)) - 4) / 16 // ≈ ±0.25
		drawSheared(img, glyph, origins[i], randomInt(6), slope)
	}

	// 噪点：只覆盖少量像素，保持人眼可读。
	for i := 0; i < 160; i++ {
		img.Set(randomInt(captchaWidth), randomInt(captchaHeight),
			color.RGBA{R: 0x94, G: 0xA3, B: 0xB8, A: 0xFF})
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", fmt.Errorf("encode captcha: %w", err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// randomInt 返回 [0, max) 的随机数；仅用于图形干扰，取模偏差可接受。
func randomInt(max int) int {
	if max <= 0 {
		return 0
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0
	}
	return int(n.Int64())
}

// scaleNearest 最近邻放大，避免引入插值依赖。
func scaleNearest(src *image.RGBA, scale int) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx()*scale, b.Dy()*scale))
	for y := 0; y < dst.Bounds().Dy(); y++ {
		for x := 0; x < dst.Bounds().Dx(); x++ {
			dst.Set(x, y, src.At(b.Min.X+x/scale, b.Min.Y+y/scale))
		}
	}
	return dst
}

// drawSheared 逐行水平偏移地叠加字符位图，形成轻微倾斜。
func drawSheared(dst, glyph *image.RGBA, dx, dy int, slope float64) {
	b := glyph.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		shift := int(math.Round(float64(y) * slope))
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := glyph.At(x, y).RGBA()
			if a == 0 {
				continue
			}
			px, py := dx+x+shift, dy+y
			// 向右/向下各补一个像素做加粗，位图字体放大后笔画更实，肉眼更易分辨。
			for _, p := range [][2]int{{px, py}, {px + 1, py}, {px, py + 1}} {
				if image.Pt(p[0], p[1]).In(dst.Bounds()) {
					dst.Set(p[0], p[1], color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(bl >> 8), A: 0xFF})
				}
			}
		}
	}
}

// drawLine 画一条干扰线（Bresenham）。
func drawLine(dst *image.RGBA, x1, y1, x2, y2 int, c color.RGBA) {
	dx, dy := abs(x2-x1), -abs(y2-y1)
	sx, sy := -1, -1
	if x1 < x2 {
		sx = 1
	}
	if y1 < y2 {
		sy = 1
	}
	err := dx + dy
	for {
		if image.Pt(x1, y1).In(dst.Bounds()) {
			dst.Set(x1, y1, c)
		}
		if x1 == x2 && y1 == y2 {
			return
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x1 += sx
		}
		if e2 <= dx {
			err += dx
			y1 += sy
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
