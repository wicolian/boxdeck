package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/url"
	"strings"
)

type pairResponse struct {
	Label string `json:"label"`
	URL   string `json:"url"`
	Token string `json:"token"`
}

func pairURL(r *http.Request) string {
	scheme := "http"
	if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") || r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	if host == "" {
		host = "localhost:8100"
	}
	return scheme + "://" + host
}

func (a *app) pairJSON(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		jsonReply(w, http.StatusMethodNotAllowed, object{"error": "Use GET to create a phone pairing"})
		return
	}
	a.tokenMu.Lock()
	defer a.tokenMu.Unlock()
	token, err := randomToken()
	if err != nil {
		jsonReply(w, http.StatusInternalServerError, object{"error": "Could not create a pairing token"})
		return
	}
	a.cfg.Tokens = append(a.cfg.Tokens, token)
	if err := saveConfig(a.cfg); err != nil {
		a.cfg.Tokens = a.cfg.Tokens[:len(a.cfg.Tokens)-1]
		jsonReply(w, http.StatusInternalServerError, object{"error": "Could not save the pairing token"})
		return
	}
	jsonReply(w, http.StatusOK, pairResponse{Label: "phone", URL: pairURL(r), Token: token})
}

func (a *app) pairPNG(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "Use GET to read the pairing QR", http.StatusMethodNotAllowed)
		return
	}
	a.tokenMu.Lock()
	defer a.tokenMu.Unlock()
	token, err := randomToken()
	if err != nil {
		http.Error(w, "Could not create a pairing token", http.StatusInternalServerError)
		return
	}
	a.cfg.Tokens = append(a.cfg.Tokens, token)
	if err := saveConfig(a.cfg); err != nil {
		a.cfg.Tokens = a.cfg.Tokens[:len(a.cfg.Tokens)-1]
		http.Error(w, "Could not save the pairing token", http.StatusInternalServerError)
		return
	}
	data, err := pairQRCode(pairLink(pairURL(r), token))
	if err != nil {
		http.Error(w, "Could not draw the pairing QR", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func pairLink(base, token string) string {
	return "boxdeck://add?url=" + url.QueryEscape(base) + "&token=" + url.QueryEscape(token)
}

// The pairing code uses QR version 8, byte mode, error correction M, and mask 0.
// Version 8 has 154 data bytes, enough for a host URL and a fresh boxdeck token.
func pairQRCode(value string) ([]byte, error) {
	data := []byte(value)
	if len(data) > 151 {
		return nil, fmt.Errorf("pairing URL is too long")
	}
	codewords, err := qrCodewords(data)
	if err != nil {
		return nil, err
	}
	modules := qrModules(codewords)
	return qrPNG(modules), nil
}

func qrCodewords(data []byte) ([]byte, error) {
	const dataBytes = 154
	bits := make([]bool, 0, dataBytes*8)
	appendBits := func(value, count int) {
		for i := count - 1; i >= 0; i-- {
			bits = append(bits, (value>>i)&1 == 1)
		}
	}
	appendBits(0b0100, 4)
	appendBits(len(data), 8)
	for _, b := range data {
		appendBits(int(b), 8)
	}
	for len(bits) < dataBytes*8 && len(bits)%8 != 0 {
		bits = append(bits, false)
	}
	pad := true
	for len(bits) < dataBytes*8 {
		if pad {
			appendBits(0xec, 8)
		} else {
			appendBits(0x11, 8)
		}
		pad = !pad
	}
	dataCodewords := make([]byte, dataBytes)
	for i := range dataCodewords {
		for j := 0; j < 8; j++ {
			if bits[i*8+j] {
				dataCodewords[i] |= 1 << (7 - j)
			}
		}
	}

	blocks := [][]byte{
		dataCodewords[:38], dataCodewords[38:76], dataCodewords[76:115], dataCodewords[115:],
	}
	const eccBytes = 22
	result := make([]byte, 0, 242)
	for i := 0; i < 39; i++ {
		for _, block := range blocks {
			if i < len(block) {
				result = append(result, block[i])
			}
		}
	}
	for i := 0; i < eccBytes; i++ {
		for _, block := range blocks {
			ecc := reedSolomon(block, eccBytes)
			result = append(result, ecc[i])
		}
	}
	return result, nil
}

var gfExp = func() [512]byte {
	var table [512]byte
	x := 1
	for i := 0; i < 255; i++ {
		table[i] = byte(x)
		x <<= 1
		if x&0x100 != 0 {
			x ^= 0x11d
		}
	}
	for i := 255; i < len(table); i++ {
		table[i] = table[i-255]
	}
	return table
}()

var gfLog = func() [256]byte {
	var table [256]byte
	for i := 0; i < 255; i++ {
		table[gfExp[i]] = byte(i)
	}
	return table
}()

func gfMultiply(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[int(gfLog[a])+int(gfLog[b])]
}

func reedSolomon(data []byte, degree int) []byte {
	generator := []byte{1}
	for i := 0; i < degree; i++ {
		factor := gfExp[i]
		next := make([]byte, len(generator)+1)
		for j, value := range generator {
			next[j] ^= value
			next[j+1] ^= gfMultiply(value, factor)
		}
		generator = next
	}
	result := make([]byte, degree)
	for _, value := range data {
		factor := value ^ result[0]
		copy(result, result[1:])
		result[degree-1] = 0
		for i := range result {
			result[i] ^= gfMultiply(generator[i+1], factor)
		}
	}
	return result
}

func qrModules(codewords []byte) [][]bool {
	const version = 8
	const size = 49
	modules := make([][]bool, size)
	reserved := make([][]bool, size)
	for i := range modules {
		modules[i] = make([]bool, size)
		reserved[i] = make([]bool, size)
	}
	set := func(row, col int, value bool) {
		if row >= 0 && row < size && col >= 0 && col < size {
			modules[row][col] = value
			reserved[row][col] = true
		}
	}
	for _, pos := range [][2]int{{0, 0}, {size - 7, 0}, {0, size - 7}} {
		for dy := -1; dy <= 7; dy++ {
			for dx := -1; dx <= 7; dx++ {
				row, col := pos[0]+dy, pos[1]+dx
				value := dx >= 0 && dx <= 6 && dy >= 0 && dy <= 6 && (dx == 0 || dx == 6 || dy == 0 || dy == 6 || dx >= 2 && dx <= 4 && dy >= 2 && dy <= 4)
				set(row, col, value)
			}
		}
	}
	for _, center := range []int{6, 24, 42} {
		for _, col := range []int{6, 24, 42} {
			if (center == 6 && col == 6) || (center == 6 && col == 42) || (center == 42 && col == 6) {
				continue
			}
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					set(center+dy, col+dx, qrMax(abs(dx), abs(dy)) != 1)
				}
			}
		}
	}
	for i := 8; i < size-8; i++ {
		if !reserved[6][i] {
			set(6, i, i%2 == 0)
		}
		if !reserved[i][6] {
			set(i, 6, i%2 == 0)
		}
	}
	set(4*version+9, 8, true)

	format := bchFormat(0)
	for i := 0; i < 15; i++ {
		bit := (format>>i)&1 == 1
		if i < 6 {
			set(i, 8, bit)
		} else if i < 8 {
			set(i+1, 8, bit)
		} else {
			set(size-15+i, 8, bit)
		}
		if i < 8 {
			set(8, size-i-1, bit)
		} else if i < 9 {
			set(8, 15-i, bit)
		} else {
			set(8, 15-i-1, bit)
		}
	}
	versionBits := bchVersion(version)
	for i := 0; i < 18; i++ {
		bit := (versionBits>>i)&1 == 1
		set(i/3, i%3+size-11, bit)
		set(i%3+size-11, i/3, bit)
	}

	bitIndex := 0
	row, direction := size-1, -1
	for col := size - 1; col > 0; col -= 2 {
		if col == 6 {
			col--
		}
		for {
			for _, currentCol := range []int{col, col - 1} {
				if reserved[row][currentCol] {
					continue
				}
				value := false
				if bitIndex < len(codewords)*8 {
					value = (codewords[bitIndex/8]>>(7-bitIndex%8))&1 == 1
				}
				if (row+currentCol)%2 == 0 {
					value = !value
				}
				modules[row][currentCol] = value
				bitIndex++
			}
			row += direction
			if row < 0 || row >= size {
				row -= direction
				direction = -direction
				break
			}
		}
	}
	return modules
}

func bchFormat(mask int) int {
	value := mask
	value <<= 10
	for bits := bitLength(value); bits >= 11; bits = bitLength(value) {
		value ^= 0x537 << (bits - 11)
	}
	return ((mask | value) ^ 0x5412) & 0x7fff
}

func bchVersion(version int) int {
	value := version << 12
	for bits := bitLength(value); bits >= 13; bits = bitLength(value) {
		value ^= 0x1f25 << (bits - 13)
	}
	return version<<12 | value
}

func bitLength(value int) int {
	length := 0
	for value != 0 {
		length++
		value >>= 1
	}
	return length
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func qrMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func qrPNG(modules [][]bool) []byte {
	size := len(modules) + 8
	image := image.NewGray(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			value := uint8(0xff)
			if y >= 4 && y < size-4 && x >= 4 && x < size-4 && modules[y-4][x-4] {
				value = 0
			}
			image.SetGray(x, y, color.Gray{Y: value})
		}
	}
	var output bytes.Buffer
	_ = png.Encode(&output, image)
	return output.Bytes()
}

func pairVectorHash(modules [][]bool) [32]byte {
	hash := sha256.New()
	for _, row := range modules {
		for _, value := range row {
			if value {
				_, _ = hash.Write([]byte{1})
			} else {
				_, _ = hash.Write([]byte{0})
			}
		}
	}
	var result [32]byte
	copy(result[:], hash.Sum(nil))
	return result
}

func writeUint32(b *bytes.Buffer, value uint32) {
	var data [4]byte
	binary.BigEndian.PutUint32(data[:], value)
	_, _ = b.Write(data[:])
}
