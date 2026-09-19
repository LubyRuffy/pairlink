package qr

import (
	"fmt"
	"image"
	"image/color"

	"github.com/LubyRuffy/pairlink/protocol"
)

// decodeQR reads a high-contrast, axis-aligned QR (the kind skip2 emits).
func decodeQR(img image.Image) (string, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 21 || h < 21 {
		return "", fmt.Errorf("qr: image too small")
	}
	for ver := 1; ver <= 20; ver++ {
		n := 21 + 4*(ver-1)
		real := n + 8 // quiet zone 4
		mods := sampleModules(img, real)
		if mods == nil || len(mods) != real {
			continue
		}
		grid := stripQuiet(mods, 4)
		if !finderOK(grid) {
			continue
		}
		if uri, err := decodeGrid(grid, ver); err == nil {
			return uri, nil
		}
	}
	return "", fmt.Errorf("qr: decode: no pairlink offer")
}

func sampleModules(img image.Image, real int) [][]bool {
	b := img.Bounds()
	size := b.Dx()
	if b.Dy() < size {
		size = b.Dy()
	}
	darkN := make([][]int, real)
	totN := make([][]int, real)
	for i := 0; i < real; i++ {
		darkN[i] = make([]int, real)
		totN[i] = make([]int, real)
	}
	// Invert skip2's Image() mapping: module = int(pixel * real / size).
	mpp := float64(real) / float64(size)
	for y := 0; y < size; y++ {
		y2 := int(float64(y) * mpp)
		if y2 >= real {
			y2 = real - 1
		}
		for x := 0; x < size; x++ {
			x2 := int(float64(x) * mpp)
			if x2 >= real {
				x2 = real - 1
			}
			totN[y2][x2]++
			if dark(img.At(b.Min.X+x, b.Min.Y+y)) {
				darkN[y2][x2]++
			}
		}
	}
	out := make([][]bool, real)
	for y := 0; y < real; y++ {
		out[y] = make([]bool, real)
		for x := 0; x < real; x++ {
			if totN[y][x] == 0 {
				continue
			}
			out[y][x] = darkN[y][x]*2 >= totN[y][x]
		}
	}
	return out
}

func dark(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	return (r+g+b)/3 < 0x8000
}

func stripQuiet(m [][]bool, q int) [][]bool {
	n := len(m) - 2*q
	if n < 21 {
		return nil
	}
	out := make([][]bool, n)
	for y := 0; y < n; y++ {
		out[y] = append([]bool(nil), m[y+q][q:q+n]...)
	}
	return out
}

func finderOK(g [][]bool) bool {
	n := len(g)
	if n < 21 {
		return false
	}
	return finderAt(g, 0, 0) && finderAt(g, n-7, 0) && finderAt(g, 0, n-7)
}

func finderAt(g [][]bool, x, y int) bool {
	// 7x7 finder: dark ring, light ring, dark center.
	pat := []string{
		"1111111",
		"1000001",
		"1011101",
		"1011101",
		"1011101",
		"1000001",
		"1111111",
	}
	for row := 0; row < 7; row++ {
		for col := 0; col < 7; col++ {
			want := pat[row][col] == '1'
			if g[y+row][x+col] != want {
				return false
			}
		}
	}
	return true
}

func decodeGrid(g [][]bool, ver int) (string, error) {
	n := len(g)
	reserved := reservedMap(n, ver)
	var last string
	for invert := 0; invert < 2; invert++ {
		for level := 0; level < 4; level++ {
			for mask := 0; mask < 8; mask++ {
				bits := unmaskBitsPolarity(g, reserved, mask, invert == 1)
				cw := packBytes(bits)
				if len(cw) < 8 {
					last = "short cw"
					continue
				}
				data, ok := deinterleave(cw, ver, level)
				if !ok {
					last = "deinterleave"
					continue
				}
				uri, err := parseBytePayload(data, ver)
				if err != nil {
					last = err.Error()
					continue
				}
				if _, err := protocol.Parse(uri); err == nil {
					return uri, nil
				}
				if len(uri) > 12 {
					last = "parse " + uri[:12]
				} else {
					last = "parse"
				}
			}
		}
	}
	return "", fmt.Errorf("qr: no payload (%s)", last)
}

func reservedMap(n, ver int) [][]bool {
	r := make([][]bool, n)
	for y := 0; y < n; y++ {
		r[y] = make([]bool, n)
	}
	mark := func(x, y int) {
		if x >= 0 && y >= 0 && x < n && y < n {
			r[y][x] = true
		}
	}
	// Finders 7x7 + 8-module separators, matching skip2 addFinderPatterns.
	paintFinder := func(x, y int) {
		for dy := 0; dy < 7; dy++ {
			for dx := 0; dx < 7; dx++ {
				mark(x+dx, y+dy)
			}
		}
	}
	paintFinder(0, 0)
	paintFinder(n-7, 0)
	paintFinder(0, n-7)
	for i := 0; i < 8; i++ {
		mark(i, 7)
		mark(7, i)
		mark(n-8+i, 7)
		mark(n-8, i)
		mark(i, n-8)
		mark(7, n-8+i)
	}
	// Alignment before timing: skip2 paints alignment while the timing
	// row is still empty, so centers on column/row 6 still get a 5x5.
	for _, ax := range alignCenters(ver) {
		for _, ay := range alignCenters(ver) {
			if r[ay][ax] {
				continue
			}
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					mark(ax+dx, ay+dy)
				}
			}
		}
	}
	for i := 8; i < n-7; i++ {
		mark(i, 6)
		mark(6, i)
	}
	for i := 0; i <= 7; i++ {
		mark(n-1-i, 8)
	}
	for i := 0; i <= 5; i++ {
		mark(8, i)
	}
	mark(8, 7)
	mark(8, 8)
	mark(7, 8)
	for i := 9; i <= 14; i++ {
		mark(14-i, 8)
	}
	for i := 8; i <= 14; i++ {
		mark(8, n-7+i-8)
	}
	mark(8, n-8)
	if ver >= 7 {
		for i := 0; i < 18; i++ {
			mark(i/3, n-11+i%3)
			mark(n-11+i%3, i/3)
		}
	}
	return r
}

func alignCenters(ver int) []int {
	if ver <= 1 {
		return nil
	}
	table := [][]int{
		nil, nil,
		{6, 18}, {6, 22}, {6, 26}, {6, 30}, {6, 34},
		{6, 22, 38}, {6, 24, 42}, {6, 26, 46}, {6, 28, 50},
		{6, 30, 54}, {6, 32, 58}, {6, 34, 62},
		{6, 26, 46, 66}, {6, 26, 48, 70}, {6, 26, 50, 74},
		{6, 30, 54, 78}, {6, 30, 56, 82}, {6, 30, 58, 86},
		{6, 34, 62, 90},
	}
	if ver < len(table) {
		return table[ver]
	}
	return nil
}

func maskBit(mask, x, y int) bool {
	switch mask {
	case 0:
		return (x+y)%2 == 0
	case 1:
		return y%2 == 0
	case 2:
		return x%3 == 0
	case 3:
		return (x+y)%3 == 0
	case 4:
		return (y/2+x/3)%2 == 0
	case 5:
		return (x*y)%2+(x*y)%3 == 0
	case 6:
		return ((x*y)%2+(x*y)%3)%2 == 0
	default:
		return ((x+y)%2+(x*y)%3)%2 == 0
	}
}

func unmaskBits(g [][]bool, reserved [][]bool, mask int) []byte {
	return unmaskBitsPolarity(g, reserved, mask, false)
}

func unmaskBitsPolarity(g [][]bool, reserved [][]bool, mask int, invert bool) []byte {
	n := len(g)
	var bits []byte
	xOffset := 1
	dirUp := true
	x := n - 2
	y := n - 1
	for x >= 0 {
		xx := x + xOffset
		if xx >= 0 && xx < n && y >= 0 && y < n && !reserved[y][xx] {
			v := g[y][xx]
			if invert {
				v = !v
			}
			if maskBit(mask, xx, y) {
				v = !v
			}
			if v {
				bits = append(bits, 1)
			} else {
				bits = append(bits, 0)
			}
		}
		if xOffset == 1 {
			xOffset = 0
			continue
		}
		xOffset = 1
		if dirUp {
			if y > 0 {
				y--
			} else {
				dirUp = false
				x -= 2
			}
		} else {
			if y < n-1 {
				y++
			} else {
				dirUp = true
				x -= 2
			}
		}
		if x == 5 {
			x--
		}
	}
	return bits
}

func packBytes(bits []byte) []byte {
	out := make([]byte, len(bits)/8)
	for i := range out {
		var b byte
		for j := 0; j < 8; j++ {
			b = b<<1 | bits[i*8+j]
		}
		out[i] = b
	}
	return out
}

func deinterleave(cw []byte, ver, level int) ([]byte, bool) {
	if ver < 1 || ver > 20 || level < 0 || level > 3 {
		return nil, false
	}
	groups := eccBlocks[ver][level]
	if len(groups) == 0 {
		return nil, false
	}
	type blk struct{ total, data, n int }
	var blocks []blk
	totalCW := 0
	dataCW := 0
	for _, g := range groups {
		n, total, data := g[0], g[1], g[2]
		for i := 0; i < n; i++ {
			blocks = append(blocks, blk{total: total, data: data})
			totalCW += total
			dataCW += data
		}
	}
	if len(cw) < totalCW {
		return nil, false
	}
	bufs := make([][]byte, len(blocks))
	for i := range bufs {
		bufs[i] = make([]byte, blocks[i].total)
	}
	pos := 0
	maxData := 0
	for _, b := range blocks {
		if b.data > maxData {
			maxData = b.data
		}
	}
	for i := 0; i < maxData; i++ {
		for bi, b := range blocks {
			if i < b.data {
				if pos >= len(cw) {
					return nil, false
				}
				bufs[bi][i] = cw[pos]
				pos++
			}
		}
	}
	maxEC := 0
	for _, b := range blocks {
		ec := b.total - b.data
		if ec > maxEC {
			maxEC = ec
		}
	}
	for i := 0; i < maxEC; i++ {
		for bi, b := range blocks {
			ec := b.total - b.data
			if i < ec {
				if pos >= len(cw) {
					return nil, false
				}
				bufs[bi][b.data+i] = cw[pos]
				pos++
			}
		}
	}
	out := make([]byte, 0, dataCW)
	for i, b := range blocks {
		out = append(out, bufs[i][:b.data]...)
	}
	return out, true
}

func parseBytePayload(data []byte, ver int) (string, error) {
	bits := make([]byte, 0, len(data)*8)
	for _, b := range data {
		for i := 7; i >= 0; i-- {
			bits = append(bits, (b>>i)&1)
		}
	}
	pos := 0
	take := func(n int) (int, bool) {
		if pos+n > len(bits) {
			return 0, false
		}
		v := 0
		for i := 0; i < n; i++ {
			v = v<<1 | int(bits[pos])
			pos++
		}
		return v, true
	}
	numLen, alNumLen, byteLen := 10, 9, 8
	switch {
	case ver >= 27:
		numLen, alNumLen, byteLen = 14, 13, 16
	case ver >= 10:
		numLen, alNumLen, byteLen = 12, 11, 16
	}
	var out []byte
	// skip2 mixes numeric / alphanumeric / byte segments. A prefix that
	// already Parses (hub+code+32-byte key, no ?lan=) used to return
	// early and drop the LAN query. Keep the longest parseable URI.
	var best string
	for {
		mode, ok := take(4)
		if !ok || mode == 0 {
			break
		}
		switch mode {
		case 1: // numeric
			n, ok := take(numLen)
			if !ok || n < 0 || n > len(bits) {
				return "", fmt.Errorf("short numeric")
			}
			for n > 0 {
				chunk := 3
				if n < 3 {
					chunk = n
				}
				bitsUsed := 1 + 3*chunk
				v, ok := take(bitsUsed)
				if !ok {
					return "", fmt.Errorf("short numeric data")
				}
				var s string
				switch chunk {
				case 3:
					s = fmt.Sprintf("%03d", v)
				case 2:
					s = fmt.Sprintf("%02d", v)
				default:
					s = fmt.Sprintf("%d", v)
				}
				out = append(out, s...)
				n -= chunk
			}
		case 2: // alphanumeric
			n, ok := take(alNumLen)
			if !ok || n < 0 || n > len(bits) {
				return "", fmt.Errorf("short alnum")
			}
			const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ $%*+-./:"
			for n > 0 {
				if n >= 2 {
					v, ok := take(11)
					if !ok || v/45 >= len(alphabet) || v%45 >= len(alphabet) {
						return "", fmt.Errorf("short alnum data")
					}
					out = append(out, alphabet[v/45], alphabet[v%45])
					n -= 2
					continue
				}
				v, ok := take(6)
				if !ok || v >= len(alphabet) {
					return "", fmt.Errorf("short alnum data")
				}
				out = append(out, alphabet[v])
				n--
			}
		case 4: // byte
			n, ok := take(byteLen)
			if !ok || n < 0 {
				return "", fmt.Errorf("short length")
			}
			if pos+n*8 > len(bits) {
				return "", fmt.Errorf("bad length")
			}
			for i := 0; i < n; i++ {
				b, ok := take(8)
				if !ok {
					return "", fmt.Errorf("short byte")
				}
				out = append(out, byte(b))
			}
		default:
			if best != "" {
				return best, nil
			}
			if len(out) == 0 {
				return "", fmt.Errorf("mode %d", mode)
			}
			return string(out), nil
		}
		if _, err := protocol.Parse(string(out)); err == nil {
			best = string(out)
		}
	}
	if best != "" {
		return best, nil
	}
	if len(out) == 0 {
		return "", fmt.Errorf("empty")
	}
	return string(out), nil
}
