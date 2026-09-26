package contacts

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/internal/errs"
)

func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

func decodedPhoto(t *testing.T, uri string) image.Image {
	t.Helper()
	payload, ok := strings.CutPrefix(uri, "data:image/jpeg;base64,")
	if !ok {
		t.Fatalf("the photo is not a JPEG data URI: %.40s", uri)
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("base64: %v", err)
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("jpeg.Decode: %v", err)
	}
	return img
}

func TestPhotoFromImageShrinksTheShorterSideAndKeepsTheShape(t *testing.T) {
	for _, tc := range []struct {
		w, h, wantW, wantH int
	}{
		{800, 600, 240, 180},
		{600, 800, 180, 240},
		{100, 400, 100, 400},
		{120, 90, 120, 90},
	} {
		uri, err := PhotoFromImage("jane.png", pngOf(t, tc.w, tc.h))
		if err != nil {
			t.Fatalf("%d×%d: %v", tc.w, tc.h, err)
		}
		b := decodedPhoto(t, uri).Bounds()
		if b.Dx() != tc.wantW || b.Dy() != tc.wantH {
			t.Errorf("%d×%d became %d×%d, want %d×%d", tc.w, tc.h, b.Dx(), b.Dy(), tc.wantW, tc.wantH)
		}
	}
}

func TestPhotoFromImageRefusesWhatIsNotAnImage(t *testing.T) {
	_, err := PhotoFromImage("notes.txt", []byte("hello"))
	var problem *errs.Problem
	if err == nil || err.Error() != "notes.txt is not an image proton can read." {
		t.Fatalf("err = %v, want a refusal", err)
	}
	if !errors.As(err, &problem) {
		t.Errorf("the refusal is not phrased for a person: %T", err)
	}
}

// withOrientation puts an EXIF block recording orientation into a JPEG, the way
// a camera writes one.
func withOrientation(t *testing.T, jpg []byte, orientation uint16) []byte {
	t.Helper()
	tiff := []byte("MM\x00\x2a\x00\x00\x00\x08")
	tiff = binary.BigEndian.AppendUint16(tiff, 1)
	tiff = binary.BigEndian.AppendUint16(tiff, 0x0112)
	tiff = binary.BigEndian.AppendUint16(tiff, 3)
	tiff = binary.BigEndian.AppendUint32(tiff, 1)
	tiff = binary.BigEndian.AppendUint16(tiff, orientation)
	tiff = append(tiff, 0, 0, 0, 0, 0, 0)
	segment := append([]byte("Exif\x00\x00"), tiff...)
	app1 := []byte{0xFF, 0xE1}
	app1 = binary.BigEndian.AppendUint16(app1, uint16(len(segment)+2))
	app1 = append(app1, segment...)
	return append(append(append([]byte{}, jpg[:2]...), app1...), jpg[2:]...)
}

// A phone stores a portrait picture on its side and says so in EXIF, so the
// photo has to come out standing up.
func TestPhotoFromImageTurnsAPictureUpright(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 400, 200))
	for y := range 200 {
		for x := range 200 {
			src.Set(x, y, color.RGBA{R: 255, A: 255})
		}
		for x := 200; x < 400; x++ {
			src.Set(x, y, color.RGBA{B: 255, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	uri, err := PhotoFromImage("jane.jpg", withOrientation(t, buf.Bytes(), 6))
	if err != nil {
		t.Fatalf("PhotoFromImage: %v", err)
	}
	img := decodedPhoto(t, uri)
	if b := img.Bounds(); b.Dx() != 180 || b.Dy() != 360 {
		t.Fatalf("an image turned a quarter came out %d×%d, want 180×360", b.Dx(), b.Dy())
	}
	// Turned clockwise, the red left half of the stored image is on top.
	if r, _, bl, _ := img.At(90, 20).RGBA(); r < bl {
		t.Error("the top of the photo is not the part the camera stored on the left")
	}
}

func TestPhotoFromURLTakesAWebAddressOnly(t *testing.T) {
	if got, err := PhotoFromURL("https://example.com/jane.png"); err != nil || got != "https://example.com/jane.png" {
		t.Errorf("PhotoFromURL = %q, %v", got, err)
	}
	for _, bad := range []string{"https://", "ftp://example.com/a.png", "https:/nohost"} {
		if _, err := PhotoFromURL(bad); err == nil {
			t.Errorf("%q was taken as a web address", bad)
		}
	}
}

func TestDescribePhotoSaysWhatTheContactHolds(t *testing.T) {
	uri, err := PhotoFromImage("jane.png", pngOf(t, 360, 480))
	if err != nil {
		t.Fatalf("PhotoFromImage: %v", err)
	}
	if got := DescribePhoto(uri); got != "JPEG, 180×240" {
		t.Errorf("DescribePhoto = %q", got)
	}
	if got := DescribePhoto("https://example.com/jane.png"); got != "https://example.com/jane.png" {
		t.Errorf("a web address was described as %q", got)
	}
	if got := DescribePhoto("data:image/heic;base64,AAAA"); got != "image/heic" {
		t.Errorf("an image that does not decode was described as %q", got)
	}
}
