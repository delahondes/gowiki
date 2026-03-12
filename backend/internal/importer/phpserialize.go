package importer

import (
	"fmt"
	"strconv"
	"strings"
)

// phpUnserialize parses a PHP serialized string into Go types.
// Supports: string, int, float, bool, null, array (associative -> map, sequential -> slice).
// This is a minimal parser sufficient for DokuWiki .meta files.
func phpUnserialize(data []byte) (any, error) {
	p := &phpParser{data: string(data), pos: 0}
	return p.parse()
}

type phpParser struct {
	data string
	pos  int
}

func (p *phpParser) parse() (any, error) {
	if p.pos >= len(p.data) {
		return nil, fmt.Errorf("unexpected end of data")
	}

	switch p.data[p.pos] {
	case 's': // s:length:"string";
		return p.parseString()
	case 'i': // i:value;
		return p.parseInt()
	case 'd': // d:value;
		return p.parseFloat()
	case 'b': // b:0; or b:1;
		return p.parseBool()
	case 'N': // N;
		return p.parseNull()
	case 'a': // a:count:{...}
		return p.parseArray()
	case 'O': // O:length:"class":count:{...} - simplified: treat as array
		return p.parseObject()
	default:
		return nil, fmt.Errorf("unknown type '%c' at pos %d", p.data[p.pos], p.pos)
	}
}

func (p *phpParser) parseString() (string, error) {
	// s:length:"string";
	if err := p.expect('s'); err != nil {
		return "", err
	}
	if err := p.expect(':'); err != nil {
		return "", err
	}
	length, err := p.readInt()
	if err != nil {
		return "", err
	}
	if err := p.expect(':'); err != nil {
		return "", err
	}
	if err := p.expect('"'); err != nil {
		return "", err
	}
	if p.pos+int(length) > len(p.data) {
		return "", fmt.Errorf("string length %d exceeds data at pos %d", length, p.pos)
	}
	s := p.data[p.pos : p.pos+int(length)]
	p.pos += int(length)
	if err := p.expect('"'); err != nil {
		return "", err
	}
	if err := p.expect(';'); err != nil {
		return "", err
	}
	return s, nil
}

func (p *phpParser) parseInt() (int64, error) {
	if err := p.expect('i'); err != nil {
		return 0, err
	}
	if err := p.expect(':'); err != nil {
		return 0, err
	}
	n, err := p.readInt()
	if err != nil {
		return 0, err
	}
	if err := p.expect(';'); err != nil {
		return 0, err
	}
	return n, nil
}

func (p *phpParser) parseFloat() (float64, error) {
	if err := p.expect('d'); err != nil {
		return 0, err
	}
	if err := p.expect(':'); err != nil {
		return 0, err
	}
	start := p.pos
	for p.pos < len(p.data) && p.data[p.pos] != ';' {
		p.pos++
	}
	f, err := strconv.ParseFloat(p.data[start:p.pos], 64)
	if err != nil {
		return 0, err
	}
	if err := p.expect(';'); err != nil {
		return 0, err
	}
	return f, nil
}

func (p *phpParser) parseBool() (bool, error) {
	if err := p.expect('b'); err != nil {
		return false, err
	}
	if err := p.expect(':'); err != nil {
		return false, err
	}
	if p.pos >= len(p.data) {
		return false, fmt.Errorf("unexpected end of data")
	}
	val := p.data[p.pos] == '1'
	p.pos++
	if err := p.expect(';'); err != nil {
		return false, err
	}
	return val, nil
}

func (p *phpParser) parseNull() (any, error) {
	if err := p.expect('N'); err != nil {
		return nil, err
	}
	if err := p.expect(';'); err != nil {
		return nil, err
	}
	return nil, nil
}

func (p *phpParser) parseArray() (any, error) {
	if err := p.expect('a'); err != nil {
		return nil, err
	}
	if err := p.expect(':'); err != nil {
		return nil, err
	}
	count, err := p.readInt()
	if err != nil {
		return nil, err
	}
	if err := p.expect(':'); err != nil {
		return nil, err
	}
	if err := p.expect('{'); err != nil {
		return nil, err
	}

	result := make(map[string]any, int(count))
	for i := int64(0); i < count; i++ {
		// Parse key (string or int)
		key, err := p.parse()
		if err != nil {
			return nil, fmt.Errorf("array key %d: %w", i, err)
		}
		// Parse value
		val, err := p.parse()
		if err != nil {
			return nil, fmt.Errorf("array value %d: %w", i, err)
		}

		// Convert key to string
		var keyStr string
		switch k := key.(type) {
		case string:
			keyStr = k
		case int64:
			keyStr = strconv.FormatInt(k, 10)
		default:
			keyStr = fmt.Sprintf("%v", k)
		}
		result[keyStr] = val
	}

	if err := p.expect('}'); err != nil {
		return nil, err
	}
	return result, nil
}

func (p *phpParser) parseObject() (any, error) {
	// O:length:"classname":count:{key;val;...}
	// Simplified: skip the class info and parse as array
	if err := p.expect('O'); err != nil {
		return nil, err
	}
	if err := p.expect(':'); err != nil {
		return nil, err
	}
	length, err := p.readInt()
	if err != nil {
		return nil, err
	}
	if err := p.expect(':'); err != nil {
		return nil, err
	}
	if err := p.expect('"'); err != nil {
		return nil, err
	}
	p.pos += int(length) // skip class name
	if err := p.expect('"'); err != nil {
		return nil, err
	}
	if err := p.expect(':'); err != nil {
		return nil, err
	}
	count, err := p.readInt()
	if err != nil {
		return nil, err
	}
	if err := p.expect(':'); err != nil {
		return nil, err
	}
	if err := p.expect('{'); err != nil {
		return nil, err
	}

	result := make(map[string]any, int(count))
	for i := int64(0); i < count; i++ {
		key, err := p.parse()
		if err != nil {
			return nil, err
		}
		val, err := p.parse()
		if err != nil {
			return nil, err
		}
		var keyStr string
		switch k := key.(type) {
		case string:
			// PHP private/protected members have null bytes in key names
			keyStr = strings.TrimLeft(k, "\x00*")
			if idx := strings.LastIndex(keyStr, "\x00"); idx >= 0 {
				keyStr = keyStr[idx+1:]
			}
		case int64:
			keyStr = strconv.FormatInt(k, 10)
		default:
			keyStr = fmt.Sprintf("%v", k)
		}
		result[keyStr] = val
	}

	if err := p.expect('}'); err != nil {
		return nil, err
	}
	return result, nil
}

func (p *phpParser) expect(ch byte) error {
	if p.pos >= len(p.data) {
		return fmt.Errorf("expected '%c' but reached end of data", ch)
	}
	if p.data[p.pos] != ch {
		return fmt.Errorf("expected '%c' but got '%c' at pos %d", ch, p.data[p.pos], p.pos)
	}
	p.pos++
	return nil
}

func (p *phpParser) readInt() (int64, error) {
	start := p.pos
	if p.pos < len(p.data) && p.data[p.pos] == '-' {
		p.pos++
	}
	for p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
		p.pos++
	}
	if p.pos == start {
		return 0, fmt.Errorf("expected integer at pos %d", start)
	}
	return strconv.ParseInt(p.data[start:p.pos], 10, 64)
}
