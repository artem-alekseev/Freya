// Package textcodec contains the text conversion needed by the RU client.
// The database/RPC boundary uses UTF-8, while the legacy client packets use
// Windows-1251 bytes for character text.
package textcodec

import "unicode/utf8"

var decodeSpecial = map[byte]rune{
	0x80: 0x20AC,
	0x82: 0x201A,
	0x83: 0x0192,
	0x84: 0x201E,
	0x85: 0x2026,
	0x86: 0x2020,
	0x87: 0x2021,
	0x88: 0x02C6,
	0x89: 0x2030,
	0x8A: 0x0160,
	0x8B: 0x2039,
	0x8C: 0x0152,
	0x8E: 0x017D,
	0x91: 0x2018,
	0x92: 0x2019,
	0x93: 0x201C,
	0x94: 0x201D,
	0x95: 0x2022,
	0x96: 0x2013,
	0x97: 0x2014,
	0x98: 0x02DC,
	0x99: 0x2122,
	0x9A: 0x0161,
	0x9B: 0x203A,
	0x9C: 0x0153,
	0x9E: 0x017E,
	0x9F: 0x0178,
	0xA0: 0x00A0,
	0xA1: 0x040E,
	0xA2: 0x045E,
	0xA3: 0x0408,
	0xA4: 0x00A4,
	0xA5: 0x0490,
	0xA6: 0x00A6,
	0xA7: 0x00A7,
	0xA8: 0x0401,
	0xA9: 0x00A9,
	0xAA: 0x0404,
	0xAB: 0x00AB,
	0xAC: 0x00AC,
	0xAD: 0x00AD,
	0xAE: 0x00AE,
	0xAF: 0x0407,
	0xB0: 0x00B0,
	0xB1: 0x00B1,
	0xB2: 0x0406,
	0xB3: 0x0456,
	0xB4: 0x0491,
	0xB5: 0x00B5,
	0xB6: 0x00B6,
	0xB7: 0x00B7,
	0xB8: 0x0451,
	0xB9: 0x2116,
	0xBA: 0x0454,
	0xBB: 0x00BB,
	0xBC: 0x0458,
	0xBD: 0x0405,
	0xBE: 0x0455,
	0xBF: 0x0457,
}

var encodeSpecial = map[rune]byte{}

func init() {
	for value, character := range decodeSpecial {
		encodeSpecial[character] = value
	}
}

// FromClient converts a byte sequence received from the RU client to UTF-8.
func FromClient(data []byte) string {
	result := make([]rune, 0, len(data))
	for _, value := range data {
		result = append(result, decodeByte(value))
	}
	return string(result)
}

// ToClient converts UTF-8 text to the Windows-1251 bytes expected by the
// client. Characters that do not exist in Windows-1251 are replaced by '?'.
func ToClient(value string) []byte {
	if !utf8.ValidString(value) {
		// Some legacy character fields are already stored as raw CP1251 bytes.
		return []byte(value)
	}

	result := make([]byte, 0, len(value))
	for len(value) > 0 {
		character, size := utf8.DecodeRuneInString(value)
		if character == utf8.RuneError && size == 1 {
			result = append(result, '?')
			value = value[1:]
			continue
		}
		result = append(result, encodeRune(character))
		value = value[size:]
	}
	return result
}

func decodeByte(value byte) rune {
	switch {
	case value < 0x80:
		return rune(value)
	case value >= 0xC0 && value <= 0xDF:
		return 0x0410 + rune(value-0xC0)
	case value >= 0xE0:
		return 0x0430 + rune(value-0xE0)
	}
	if character, ok := decodeSpecial[value]; ok {
		return character
	}
	return utf8.RuneError
}

func encodeRune(character rune) byte {
	switch {
	case character < 0x80:
		return byte(character)
	case character >= 0x0410 && character <= 0x042F:
		return byte(0xC0 + character - 0x0410)
	case character >= 0x0430 && character <= 0x044F:
		return byte(0xE0 + character - 0x0430)
	}
	if value, ok := encodeSpecial[character]; ok {
		return value
	}
	return '?'
}
