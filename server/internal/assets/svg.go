package assets

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
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
	// Bound document elements, path segments and numeric-list operands in total.
	maxSVGGeometry = 10_000
)

var svgElements = map[string]bool{
	"svg": true, "g": true, "path": true, "rect": true,
	"circle": true, "ellipse": true, "line": true,
	"polyline": true, "polygon": true, "title": true, "desc": true,
	"defs": true, "clipPath": true,
}

const staticSVGGuidance = "Upload a static SVG using paths, basic shapes, and local clip paths. Remove styles, links, scripts, and embedded content."

var errStaticSVG = errors.New("unsupported static SVG")
var svgClipID = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)
var svgClipReference = regexp.MustCompile(`^url\(#[A-Za-z_][A-Za-z0-9_-]*\)$`)

var svgNumber = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?`)
var svgColor = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)
var svgDeclaration = regexp.MustCompile(`^version\s*=\s*(?:"1\.0"|'1\.0')(?:\s+encoding\s*=\s*(?:"UTF-8"|'UTF-8'))?(?:\s+standalone\s*=\s*(?:"(?:yes|no)"|'(?:yes|no)'))?\s*$`)

// svgBuffer prevents XML escaping from growing the encoded asset past the limit.
type svgBuffer struct{ bytes.Buffer }

func (b *svgBuffer) Write(p []byte) (int, error) {
	if len(p) > MaxFileSizeImage-b.Len() {
		return 0, errStaticSVG
	}
	n, err := b.Buffer.Write(p)
	if err != nil {
		return n, fmt.Errorf("write sanitized SVG: %w", err)
	}
	return n, nil
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
	geometry := maxSVGGeometry
	clipIDs := map[string]bool{}
	clipReferences := map[string]bool{}
	inClip := false
	clipShapes := 0
	defsStartCount := 0
	for count := 0; ; count++ {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
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
			parent := ""
			if len(stack) > 0 {
				parent = stack[len(stack)-1]
			}
			// Only root-level definitions and direct, leaf shapes are supported.
			if (name == "defs" && parent != "svg") ||
				(name == "clipPath" && parent != "defs") ||
				(parent == "defs" && name != "clipPath") ||
				(inClip && (parent != "clipPath" || !svgStaticShape(name))) {
				return nil, errStaticSVG
			}
			if name == "defs" {
				defsStartCount = len(clipIDs)
			}
			if inClip {
				clipShapes++
			}
			if name == "clipPath" {
				inClip = true
				clipShapes = 0
			}
			geometry--
			if geometry < 0 {
				return nil, errStaticSVG
			}
			attrs := make([]xml.Attr, 0, len(t.Attr)+1)
			if len(stack) == 0 {
				attrs = append(attrs, xml.Attr{Name: xml.Name{Space: "", Local: "xmlns"}, Value: svgNamespace})
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
				if a.Name.Space != "" || !validSVGAttribute(name, a.Name.Local, a.Value, &geometry) {
					return nil, errStaticSVG
				}
				if a.Name.Local == "id" {
					if clipIDs[a.Value] {
						return nil, errStaticSVG
					}
					clipIDs[a.Value] = true
				}
				if a.Name.Local == "clip-path" {
					if inClip {
						return nil, errStaticSVG
					}
					clipReferences[a.Value[5:len(a.Value)-1]] = true
				}
				attrs = append(attrs, a)
			}
			if name == "clipPath" && !seen[xml.Name{Space: "", Local: "id"}] {
				return nil, errStaticSVG
			}
			stack = append(stack, name)
			token = xml.StartElement{Name: xml.Name{Space: "", Local: name}, Attr: attrs}
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, errStaticSVG
			}
			if t.Name.Local == "clipPath" {
				if clipShapes == 0 {
					return nil, errStaticSVG
				}
				inClip = false
			}
			if t.Name.Local == "defs" && len(clipIDs) == defsStartCount {
				return nil, errStaticSVG
			}
			stack = stack[:len(stack)-1]
			token = xml.EndElement{Name: xml.Name{Space: "", Local: t.Name.Local}}
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
	// Resolve after parsing to allow forward references, but only to clipPath IDs.
	for id := range clipReferences {
		if !clipIDs[id] {
			return nil, errStaticSVG
		}
	}
	if err := encoder.Flush(); err != nil {
		return nil, errStaticSVG
	}
	return output.Bytes(), nil
}

func svgNumbers(s string, budget *int) ([]float64, bool) {
	var values []float64
	for s != "" {
		s = strings.TrimLeft(s, " \t\r\n,")
		if s == "" {
			break
		}
		n := svgNumber.FindString(s)
		if n == "" {
			return nil, false
		}
		v, err := strconv.ParseFloat(n, 64)
		if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
			return nil, false
		}
		*budget--
		if *budget < 0 {
			return nil, false
		}
		values = append(values, v)
		s = s[len(n):]
	}
	return values, true
}

func svgStaticShape(name string) bool {
	switch name {
	case "path", "rect", "circle", "ellipse", "line", "polyline", "polygon":
		return true
	default:
		return false
	}
}

func validSVGAttribute(element, name, value string, budget *int) bool {
	if element == "defs" {
		return false
	}
	if element == "clipPath" {
		return name == "id" && svgClipID.MatchString(value)
	}
	if name == "clip-path" {
		return (element == "g" || svgStaticShape(element)) && svgClipReference.MatchString(value)
	}
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
		return validSVGPath(value, budget)
	case "points":
		if element != "polygon" && element != "polyline" {
			return false
		}
		ns, ok := svgNumbers(value, budget)
		return ok && len(ns) >= 2 && len(ns)%2 == 0
	case "viewBox":
		if element != "svg" {
			return false
		}
		ns, ok := svgNumbers(value, budget)
		return ok && len(ns) == 4 && ns[2] > 0 && ns[3] > 0
	case "transform":
		return validSVGTransform(value, budget)
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

func validSVGTransform(s string, budget *int) bool {
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
		ns, ok := svgNumbers(s[:i], budget)
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

// validSVGPath checks segment arities without interpreting or rendering geometry.
// Each repeated argument group counts as a segment, including implicit lineto.
func validSVGPath(s string, budget *int) bool {
	var command byte
	first := true
	for {
		s = strings.TrimLeft(s, " \t\r\n")
		if s == "" {
			return !first
		}
		if strings.ContainsRune("MmZzLlHhVvCcSsQqTtAa", rune(s[0])) {
			command = s[0]
			s = s[1:]
		} else if command == 0 {
			return false
		}
		if first && command != 'M' && command != 'm' {
			return false
		}
		first = false
		*budget--
		if *budget < 0 {
			return false
		}
		arity := 0
		switch command {
		case 'Z', 'z':
			command = 0
			continue
		case 'H', 'h', 'V', 'v':
			arity = 1
		case 'M', 'm', 'L', 'l', 'T', 't':
			arity = 2
		case 'S', 's', 'Q', 'q':
			arity = 4
		case 'C', 'c':
			arity = 6
		case 'A', 'a':
			arity = 7
		default:
			return false
		}
		for i := range arity {
			s = strings.TrimLeft(s, " \t\r\n")
			if i > 0 && strings.HasPrefix(s, ",") {
				s = strings.TrimLeft(s[1:], " \t\r\n")
			}
			n := svgNumber.FindString(s)
			arc := command == 'A' || command == 'a'
			if arc && (i == 3 || i == 4) {
				// Arc flags are single digits; compact forms such as 0110 are valid.
				if s == "" || (s[0] != '0' && s[0] != '1') {
					return false
				}
				n = s[:1]
			}
			if n == "" {
				return false
			}
			v, err := strconv.ParseFloat(n, 64)
			if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
				return false
			}
			*budget--
			if *budget < 0 {
				return false
			}
			s = s[len(n):]
		}
		switch command {
		case 'M':
			command = 'L'
		case 'm':
			command = 'l'
		}
		s = strings.TrimLeft(s, " \t\r\n")
		if strings.HasPrefix(s, ",") {
			s = strings.TrimLeft(s[1:], " \t\r\n")
			if s == "" || svgNumber.FindString(s) == "" {
				return false
			}
		}
	}
}
