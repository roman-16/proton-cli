package contacts

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"net/url"
	"strings"

	"github.com/roman-16/proton-cli/internal/errs"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	PhotoSide      = 180
	PhotoFormats   = "JPEG, PNG, GIF or WebP"
	photoQuality   = 90
	photoMaxPixels = 100_000_000
)

func PhotoFromImage(name string, data []byte) (string, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", errs.Problemf("%s is not an image proton can read.", name).Hint(PhotoFormats)
	}
	if cfg.Width*cfg.Height > photoMaxPixels {
		return "", errs.Problemf("%s is %d×%d pixels, which is too large to read.", name, cfg.Width, cfg.Height)
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", errs.Problemf("%s is a damaged image: %v.", name, err)
	}
	orientation := exifOrientation(data)
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if orientation >= 5 {
		w, h = h, w
	}
	tw, th := fitShorterSide(w, h, PhotoSide)
	if orientation >= 5 {
		tw, th = th, tw
	}
	scaled := image.NewRGBA(image.Rect(0, 0, tw, th))
	draw.Draw(scaled, scaled.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.CatmullRom.Scale(scaled, scaled.Bounds(), src, b, draw.Over, nil)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, upright(scaled, orientation), &jpeg.Options{Quality: photoQuality}); err != nil {
		return "", err
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(out.Bytes()), nil
}

func PhotoFromURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errs.Problemf("%s is not a web address a photo can be loaded from.", raw)
	}
	return raw, nil
}

func IsPhotoURL(raw string) bool {
	lower := strings.ToLower(raw)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func DescribePhoto(uri string) string {
	if uri == "" || !strings.HasPrefix(strings.ToLower(uri), "data:") {
		return uri
	}
	meta, payload, ok := strings.Cut(uri[len("data:"):], ",")
	media := strings.TrimSuffix(strings.ToLower(meta), ";base64")
	if !ok {
		return media
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return media
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return media
	}
	return fmt.Sprintf("%s, %d×%d", strings.ToUpper(format), cfg.Width, cfg.Height)
}

func fitShorterSide(w, h, side int) (int, int) {
	shorter := min(w, h)
	if shorter <= side {
		return w, h
	}
	return max(1, w*side/shorter), max(1, h*side/shorter)
}

// A camera stores a picture as its sensor saw it and records in the EXIF block
// how to turn it, which a decoder ignores.
func exifOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			return 1
		}
		marker := data[i+1]
		size := int(binary.BigEndian.Uint16(data[i+2:]))
		if marker == 0xDA || size < 2 || i+2+size > len(data) {
			return 1
		}
		segment := data[i+4 : i+2+size]
		if marker == 0xE1 && bytes.HasPrefix(segment, []byte("Exif\x00\x00")) {
			return tiffOrientation(segment[6:])
		}
		i += 2 + size
	}
	return 1
}

func tiffOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	dir := int(order.Uint32(tiff[4:]))
	if dir+2 > len(tiff) {
		return 1
	}
	count := int(order.Uint16(tiff[dir:]))
	for n := range count {
		entry := dir + 2 + n*12
		if entry+12 > len(tiff) {
			return 1
		}
		if order.Uint16(tiff[entry:]) == 0x0112 {
			if v := int(order.Uint16(tiff[entry+8:])); v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}

func upright(src *image.RGBA, orientation int) image.Image {
	if orientation <= 1 || orientation > 8 {
		return src
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	ow, oh := w, h
	if orientation >= 5 {
		ow, oh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, ow, oh))
	for y := range oh {
		for x := range ow {
			var sx, sy int
			switch orientation {
			case 2:
				sx, sy = w-1-x, y
			case 3:
				sx, sy = w-1-x, h-1-y
			case 4:
				sx, sy = x, h-1-y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, h-1-x
			case 7:
				sx, sy = w-1-y, h-1-x
			case 8:
				sx, sy = w-1-y, x
			}
			dst.SetRGBA(x, y, src.RGBAAt(sx, sy))
		}
	}
	return dst
}
