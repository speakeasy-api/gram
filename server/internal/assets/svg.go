package assets

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
)

const (
	svgNamespace = "http://www.w3.org/2000/svg"
	maxSVGDepth  = 64
	maxSVGTokens = 100_000
)

var svgElements = map[string]bool{
	"svg": true, "g": true, "path": true, "rect": true,
	"circle": true, "ellipse": true, "line": true,
	"polyline": true, "polygon": true, "title": true, "desc": true,
}

var errStaticSVG = errors.New("Upload a static SVG using paths and basic shapes; styles, links, and embedded content are not supported.")
var svgNumber = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?`)
var svgColor = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)
var svgDeclaration = regexp.MustCompile(`^version\s*=\s*(?:"1\.0"|'1\.0')(?:\s+encoding\s*=\s*(?:"UTF-8"|'UTF-8'))?(?:\s+standalone\s*=\s*(?:"(?:yes|no)"|'(?:yes|no)'))?\s*$`)

// svgBuffer prevents XML escaping from growing the encoded asset past the limit.
type svgBuffer struct{ bytes.Buffer }

func (b *svgBuffer) Write(p []byte) (int, error) {
	if len(p) > MaxFileSizeImage-b.Len() {
		return 0, errStaticSVG
	}
	return b.Buffer.Write(p)
}

// sanitizeSVG accepts only self-contained static shapes; it never returns partial output.
func sanitizeSVG(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data) > MaxFileSizeImage {
		return nil, errStaticSVG
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = true
	var output svgBuffer
	encoder := xml.NewEncoder(&output)
	var stack []string
	rooted := false
	for count := 0; ; count++ {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil || count >= maxSVGTokens {
			return nil, errStaticSVG
		}
		switch t := token.(type) {
		case xml.StartElement:
			name := t.Name.Local
			if t.Name.Space != svgNamespace || !svgElements[name] || len(stack) >= maxSVGDepth {
				return nil, errStaticSVG
			}
			if len(stack) == 0 {
				if rooted || name != "svg" {
					return nil, errStaticSVG
				}
				rooted = true
			} else if name == "svg" || stack[len(stack)-1] == "title" || stack[len(stack)-1] == "desc" {
				return nil, errStaticSVG
			}
			attrs := make([]xml.Attr, 0, len(t.Attr)+1)
			if len(stack) == 0 {
				attrs = append(attrs, xml.Attr{Name: xml.Name{Local: "xmlns"}, Value: svgNamespace})
			}
			seen := map[xml.Name]bool{}
			for _, a := range t.Attr {
				if seen[a.Name] {
					return nil, errStaticSVG
				}
				seen[a.Name] = true
				if a.Name.Local == "xmlns" && a.Name.Space == "" {
					if a.Value != svgNamespace {
						return nil, errStaticSVG
					}
					continue
				}
				if a.Name.Space != "" || !validSVGAttribute(name, a.Name.Local, a.Value) {
					return nil, errStaticSVG
				}
				attrs = append(attrs, a)
			}
			stack = append(stack, name)
			token = xml.StartElement{Name: xml.Name{Local: name}, Attr: attrs}
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, errStaticSVG
			}
			stack = stack[:len(stack)-1]
			token = xml.EndElement{Name: xml.Name{Local: t.Name.Local}}
		case xml.CharData:
			if strings.Trim(string(t), " \t\r\n") != "" && (len(stack) == 0 || (stack[len(stack)-1] != "title" && stack[len(stack)-1] != "desc")) {
				return nil, errStaticSVG
			}
			if len(stack) == 0 {
				continue
			}
		case xml.Comment:
			continue
		case xml.ProcInst:
			if count != 0 || t.Target != "xml" || !svgDeclaration.Match(t.Inst) {
				return nil, errStaticSVG
			}
			continue
		default:
			return nil, errStaticSVG
		}
		if err := encoder.EncodeToken(token); err != nil {
			return nil, errStaticSVG
		}
	}
	if !rooted || len(stack) != 0 {
		return nil, errStaticSVG
	}
	if err := encoder.Flush(); err != nil {
		return nil, errStaticSVG
	}
	return output.Bytes(), nil
}

func svgNumbers(s string, path bool) ([]float64, bool) {
	var values []float64
	for s != "" {
		s = strings.TrimLeft(s, " \t\r\n,")
		if s == "" {
			break
		}
		if path && strings.ContainsRune("MmZzLlHhVvCcSsQqTtAa", rune(s[0])) {
			s = s[1:]
			continue
		}
		n := svgNumber.FindString(s)
		if n == "" {
			return nil, false
		}
		v, err := strconv.ParseFloat(n, 64)
		if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
			return nil, false
		}
		if !path {
			values = append(values, v)
		}
		s = s[len(n):]
	}
	return values, true
}

func validSVGAttribute(element, name, value string) bool {
	value = strings.TrimSpace(value)
	switch name {
	case "fill", "stroke":
		return value == "none" || value == "currentColor" || value == "black" || value == "white" || value == "transparent" || svgColor.MatchString(value)
	case "fill-rule", "clip-rule":
		return value == "nonzero" || value == "evenodd"
	case "stroke-linecap":
		return value == "butt" || value == "round" || value == "square"
	case "stroke-linejoin":
		return value == "miter" || value == "round" || value == "bevel"
	case "d":
		if element != "path" || value == "" || !strings.ContainsRune("Mm", rune(value[0])) {
			return false
		}
		_, ok := svgNumbers(value, true)
		return ok
	case "points":
		if element != "polygon" && element != "polyline" {
			return false
		}
		ns, ok := svgNumbers(value, false)
		return ok && len(ns) >= 2 && len(ns)%2 == 0
	case "viewBox":
		if element != "svg" {
			return false
		}
		ns, ok := svgNumbers(value, false)
		return ok && len(ns) == 4 && ns[2] > 0 && ns[3] > 0
	case "transform":
		return validSVGTransform(value)
	}
	// Geometry is element-specific; presentation lengths apply to every allowed element.
	allowed := false
	switch name {
	case "x", "y":
		allowed = element == "svg" || element == "rect"
	case "width", "height":
		allowed = element == "svg" || element == "rect"
	case "cx", "cy":
		allowed = element == "circle" || element == "ellipse"
	case "r":
		allowed = element == "circle"
	case "rx", "ry":
		allowed = element == "rect" || element == "ellipse"
	case "x1", "y1", "x2", "y2":
		allowed = element == "line"
	case "stroke-width", "stroke-miterlimit", "opacity", "fill-opacity", "stroke-opacity":
		allowed = true
	}
	if !allowed {
		return false
	}
	opacity := strings.Contains(name, "opacity")
	if !opacity && name != "stroke-miterlimit" {
		value = strings.TrimSuffix(value, "px")
	}
	n := svgNumber.FindString(value)
	if n == "" || n != value {
		return false
	}
	v, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
		return false
	}
	switch {
	case opacity:
		return v >= 0 && v <= 1
	case name == "stroke-miterlimit":
		return v >= 1
	case name == "r" || ((name == "rx" || name == "ry") && element == "ellipse") || ((name == "width" || name == "height") && element == "svg"):
		return v > 0
	case name == "width" || name == "height" || name == "rx" || name == "ry" || name == "stroke-width":
		return v >= 0
	default:
		return true
	}
}

func validSVGTransform(s string) bool {
	if s == "" {
		return false
	}
	for s != "" {
		i := strings.IndexByte(s, '(')
		if i < 0 {
			return false
		}
		name := strings.TrimSpace(s[:i])
		s = s[i+1:]
		i = strings.IndexByte(s, ')')
		if i < 0 {
			return false
		}
		ns, ok := svgNumbers(s[:i], false)
		if !ok {
			return false
		}
		n := len(ns)
		switch name {
		case "matrix":
			ok = n == 6
		case "translate", "scale":
			ok = n == 1 || n == 2
		case "rotate":
			ok = n == 1 || n == 3
		case "skewX", "skewY":
			ok = n == 1
		default:
			return false
		}
		if !ok {
			return false
		}
		s = strings.TrimLeft(s[i+1:], " \t\r\n,")
	}
	return true
}
