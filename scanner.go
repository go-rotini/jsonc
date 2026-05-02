package jsonc

import (
	"bytes"
	"fmt"
)

// scanner converts JSONC source bytes into a slice of tokens. It is synchronous
// (no goroutines), single-pass, and produces position-annotated tokens
// preserving comment text.
//
// The scanner accepts JWCC by default (line and block comments, optional
// trailing commas). When strictJSON is true, comments and trailing commas are
// reported as syntax errors with a [StrictJSONError]; the scanner itself does
// not detect trailing commas because they appear in structural position only
// the parser distinguishes — but the scanner reports any comment under
// strict mode.
type scanner struct {
	src    []byte
	pos    int // byte offset in src
	line   int // 1-indexed line number
	col    int // 1-indexed column on the current line
	tokens []token

	strictJSON bool // when true, line/block comments are rejected
}

// newScanner returns a scanner ready to tokenize src. It strips a UTF-8 BOM
// at byte 0 and rejects non-UTF-8 BOMs (UTF-16/UTF-32) with a clear
// [SyntaxError] directing the caller to transcode first.
func newScanner(src []byte) (*scanner, error) {
	src, err := stripBOM(src)
	if err != nil {
		return nil, err
	}
	return &scanner{
		src:  src,
		line: 1,
		col:  1,
	}, nil
}

// stripBOM removes a UTF-8 BOM if present at byte 0 and returns the remaining
// bytes. Non-UTF-8 BOMs (UTF-16 LE/BE, UTF-32 LE/BE) cause a [SyntaxError]
// since this package is UTF-8 only per RFC 8259 §8.1.
func stripBOM(src []byte) ([]byte, error) {
	switch {
	case len(src) >= 3 && src[0] == 0xEF && src[1] == 0xBB && src[2] == 0xBF:
		return src[3:], nil
	case len(src) >= 4 && src[0] == 0x00 && src[1] == 0x00 && src[2] == 0xFE && src[3] == 0xFF:
		return nil, &SyntaxError{Message: "UTF-32 BE input is not supported; transcode to UTF-8 first"}
	case len(src) >= 4 && src[0] == 0xFF && src[1] == 0xFE && src[2] == 0x00 && src[3] == 0x00:
		return nil, &SyntaxError{Message: "UTF-32 LE input is not supported; transcode to UTF-8 first"}
	case len(src) >= 2 && src[0] == 0xFE && src[1] == 0xFF:
		return nil, &SyntaxError{Message: "UTF-16 BE input is not supported; transcode to UTF-8 first"}
	case len(src) >= 2 && src[0] == 0xFF && src[1] == 0xFE:
		return nil, &SyntaxError{Message: "UTF-16 LE input is not supported; transcode to UTF-8 first"}
	}
	return src, nil
}

// scan tokenizes the entire input. It returns the collected token slice or the
// first error encountered.
func (s *scanner) scan() ([]token, error) {
	s.emit(tokenStreamStart, "")
	for !s.atEnd() {
		if err := s.scanNext(); err != nil {
			return nil, err
		}
	}
	s.emit(tokenEOF, "")
	s.emit(tokenStreamEnd, "")
	return s.tokens, nil
}

func (s *scanner) scanNext() error {
	ch := s.peek()
	switch {
	case ch == ' ' || ch == '\t':
		s.advance()
	case ch == '\r':
		// CR or CRLF: emit a single newline token. Bare CR is also accepted.
		s.advance()
		if !s.atEnd() && s.peek() == '\n' {
			s.advance()
		}
		s.emit(tokenNewline, "")
	case ch == '\n':
		s.advance()
		s.emit(tokenNewline, "")
	case ch == '/':
		return s.scanComment()
	case ch == '{':
		s.emitAdvance(tokenObjectStart, "{")
	case ch == '}':
		s.emitAdvance(tokenObjectEnd, "}")
	case ch == '[':
		s.emitAdvance(tokenArrayStart, "[")
	case ch == ']':
		s.emitAdvance(tokenArrayEnd, "]")
	case ch == ':':
		s.emitAdvance(tokenNameSeparator, ":")
	case ch == ',':
		s.emitAdvance(tokenValueSeparator, ",")
	case ch == '"':
		return s.scanString()
	case ch == '-' || (ch >= '0' && ch <= '9'):
		return s.scanNumber()
	case ch == 't':
		return s.scanLiteralWord("true", tokenTrue)
	case ch == 'f':
		return s.scanLiteralWord("false", tokenFalse)
	case ch == 'n':
		return s.scanLiteralWord("null", tokenNull)
	default:
		return s.syntaxError(fmt.Sprintf("unexpected character %q", ch))
	}
	return nil
}

// scanComment handles either a line comment (// ...) or a block comment
// (/* ... */). The leading '/' has not been consumed.
func (s *scanner) scanComment() error {
	startPos := s.position()
	if s.pos+1 >= len(s.src) {
		return s.syntaxError("unexpected '/' (expected '//' or '/*')")
	}
	switch s.src[s.pos+1] {
	case '/':
		return s.scanLineComment(startPos)
	case '*':
		return s.scanBlockComment(startPos)
	default:
		return s.syntaxError(fmt.Sprintf("unexpected character %q after '/'", s.src[s.pos+1]))
	}
}

// scanLineComment scans `// ... <line ending>`. The '//' has not been consumed.
// Stored comment text excludes the leading '//' and the terminating line ending.
// CR and CRLF line endings are normalized to LF in stored text (although
// stored text does NOT include the terminating ending itself).
func (s *scanner) scanLineComment(startPos Position) error {
	if s.strictJSON {
		return &StrictJSONError{Feature: "// line comment", Pos: startPos}
	}
	s.advance() // consume first '/'
	s.advance() // consume second '/'
	textStart := s.pos
	for !s.atEnd() {
		ch := s.src[s.pos]
		if ch == '\n' || ch == '\r' {
			break
		}
		// Reject control chars (other than tab) inside comments.
		if ch < 0x20 && ch != '\t' {
			return s.syntaxError(fmt.Sprintf("control character U+%04X in line comment", ch))
		}
		s.advance()
	}
	text := string(s.src[textStart:s.pos])
	// Note: the line ending itself is not consumed here; the main scan loop
	// will produce a tokenNewline for it on the next iteration.
	s.tokens = append(s.tokens, token{
		kind:  tokenLineComment,
		value: text,
		pos:   startPos,
	})
	return nil
}

// scanBlockComment scans `/* ... */`. The '/*' has not been consumed.
// Block comments cannot be nested. CR/CRLF inside the comment body are
// normalized to LF in stored text.
func (s *scanner) scanBlockComment(startPos Position) error {
	if s.strictJSON {
		return &StrictJSONError{Feature: "/* */ block comment", Pos: startPos}
	}
	s.advance() // consume '/'
	s.advance() // consume '*'
	textStart := s.pos
	var buf []byte
	hasCR := false
	for !s.atEnd() {
		ch := s.src[s.pos]
		// Look for closing */
		if ch == '*' && s.pos+1 < len(s.src) && s.src[s.pos+1] == '/' {
			var text string
			if hasCR {
				text = string(buf)
			} else {
				text = string(s.src[textStart:s.pos])
			}
			s.advance() // consume '*'
			s.advance() // consume '/'
			s.tokens = append(s.tokens, token{
				kind:  tokenBlockComment,
				value: text,
				pos:   startPos,
			})
			return nil
		}
		// Normalize CR / CRLF → LF in stored comment text.
		if ch == '\r' {
			if !hasCR {
				buf = append(buf, s.src[textStart:s.pos]...)
				hasCR = true
			}
			buf = append(buf, '\n')
			s.advance()
			if !s.atEnd() && s.src[s.pos] == '\n' {
				s.advance()
			}
			continue
		}
		// Reject control chars (other than tab/LF/CR) inside comments.
		if ch < 0x20 && ch != '\t' && ch != '\n' {
			return s.syntaxError(fmt.Sprintf("control character U+%04X in block comment", ch))
		}
		if hasCR {
			buf = append(buf, ch)
		}
		s.advance()
	}
	return s.syntaxErrorAt(startPos, "unterminated block comment")
}

// scanString tokenizes a double-quoted string. The opening '"' has not been
// consumed. The token's value contains the source bytes including the
// surrounding quotes (escape sequences validated but kept verbatim;
// unescaping is done lazily at decode time).
func (s *scanner) scanString() error {
	startPos := s.position()
	s.advance() // consume opening '"'
	for !s.atEnd() {
		ch := s.src[s.pos]
		switch {
		case ch == '"':
			s.advance() // consume closing '"'
			value := string(s.src[startPos.Offset:s.pos])
			s.tokens = append(s.tokens, token{
				kind:  tokenString,
				value: value,
				pos:   startPos,
			})
			return nil
		case ch == '\\':
			if err := s.scanEscape(); err != nil {
				return err
			}
		case ch < 0x20:
			return s.syntaxError(fmt.Sprintf("control character U+%04X in string", ch))
		default:
			s.advance()
		}
	}
	return s.syntaxErrorAt(startPos, "unterminated string")
}

// scanEscape validates a backslash escape sequence inside a string. The
// backslash has not been consumed. Recognized escapes:
//   - \" \\ \/ \b \f \n \r \t
//   - \uXXXX (exactly 4 hex digits)
//
// Lone surrogates are accepted per RFC 8259 §8.2 — the U+FFFD substitution
// happens at decode time, not at scan time.
func (s *scanner) scanEscape() error {
	escPos := s.position()
	s.advance() // consume backslash
	if s.atEnd() {
		return s.syntaxErrorAt(escPos, "incomplete escape sequence at end of input")
	}
	ch := s.src[s.pos]
	switch ch {
	case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
		s.advance()
		return nil
	case 'u':
		s.advance() // consume 'u'
		if s.pos+4 > len(s.src) {
			return s.syntaxErrorAt(escPos, "incomplete \\u escape (need 4 hex digits)")
		}
		for i := range 4 {
			c := s.src[s.pos+i]
			if !isHex(c) {
				return s.syntaxErrorAt(escPos, fmt.Sprintf("invalid hex digit %q in \\u escape", c))
			}
		}
		for range 4 {
			s.advance()
		}
		return nil
	default:
		return s.syntaxErrorAt(escPos, fmt.Sprintf("unknown escape sequence \\%c", ch))
	}
}

// scanNumber tokenizes a number per RFC 8259 §6:
//
//	number = [ minus ] int [ frac ] [ exp ]
//	int    = zero / ( digit1-9 *DIGIT )
//	frac   = "." 1*DIGIT
//	exp    = ( "e" / "E" ) [ minus / plus ] 1*DIGIT
//
// Leading '+' is rejected. Leading zeros on multi-digit integers are rejected.
func (s *scanner) scanNumber() error {
	startPos := s.position()
	start := s.pos

	// Optional minus.
	if s.src[s.pos] == '-' {
		s.advance()
		if s.atEnd() {
			return s.syntaxErrorAt(startPos, "expected digit after '-'")
		}
	}

	// Integer part.
	if s.atEnd() {
		return s.syntaxErrorAt(startPos, "expected digit in number")
	}
	first := s.src[s.pos]
	if first < '0' || first > '9' {
		return s.syntaxErrorAt(startPos, fmt.Sprintf("expected digit, got %q", first))
	}
	if first == '0' {
		s.advance()
		// After a leading 0, the next char must NOT be a digit (no leading zeros).
		if !s.atEnd() {
			c := s.src[s.pos]
			if c >= '0' && c <= '9' {
				return s.syntaxErrorAt(startPos, "leading zeros are not allowed in numbers")
			}
		}
	} else {
		// 1-9 followed by 0+ digits.
		s.advance()
		for !s.atEnd() {
			c := s.src[s.pos]
			if c < '0' || c > '9' {
				break
			}
			s.advance()
		}
	}

	// Optional fraction.
	if !s.atEnd() && s.src[s.pos] == '.' {
		s.advance()
		if s.atEnd() {
			return s.syntaxErrorAt(startPos, "expected digit after '.'")
		}
		c := s.src[s.pos]
		if c < '0' || c > '9' {
			return s.syntaxErrorAt(startPos, fmt.Sprintf("expected digit after '.', got %q", c))
		}
		for !s.atEnd() {
			c := s.src[s.pos]
			if c < '0' || c > '9' {
				break
			}
			s.advance()
		}
	}

	// Optional exponent.
	if !s.atEnd() && (s.src[s.pos] == 'e' || s.src[s.pos] == 'E') {
		s.advance()
		if s.atEnd() {
			return s.syntaxErrorAt(startPos, "expected digit in exponent")
		}
		if s.src[s.pos] == '+' || s.src[s.pos] == '-' {
			s.advance()
			if s.atEnd() {
				return s.syntaxErrorAt(startPos, "expected digit after exponent sign")
			}
		}
		c := s.src[s.pos]
		if c < '0' || c > '9' {
			return s.syntaxErrorAt(startPos, fmt.Sprintf("expected digit in exponent, got %q", c))
		}
		for !s.atEnd() {
			c := s.src[s.pos]
			if c < '0' || c > '9' {
				break
			}
			s.advance()
		}
	}

	s.tokens = append(s.tokens, token{
		kind:  tokenNumber,
		value: string(s.src[start:s.pos]),
		pos:   startPos,
	})
	return nil
}

// scanLiteralWord scans an exact keyword (`true`, `false`, or `null`) and
// emits the given token kind. The keyword must be followed by a non-letter
// to prevent matching prefixes (e.g., `nullable` is not `null`-prefixed).
func (s *scanner) scanLiteralWord(word string, kind tokenKind) error {
	startPos := s.position()
	if s.pos+len(word) > len(s.src) {
		return s.syntaxError(fmt.Sprintf("expected %q", word))
	}
	if !bytes.Equal(s.src[s.pos:s.pos+len(word)], []byte(word)) {
		return s.syntaxError(fmt.Sprintf("expected %q", word))
	}
	// Boundary check — keyword must not be a prefix of an identifier.
	if s.pos+len(word) < len(s.src) {
		next := s.src[s.pos+len(word)]
		if isLetter(next) || isDigit(next) || next == '_' {
			return s.syntaxError(fmt.Sprintf("expected %q (followed by non-identifier character)", word))
		}
	}
	for range word {
		s.advance()
	}
	s.tokens = append(s.tokens, token{
		kind:  kind,
		value: word,
		pos:   startPos,
	})
	return nil
}

// emit appends a token at the current position (without advancing).
func (s *scanner) emit(kind tokenKind, value string) {
	s.tokens = append(s.tokens, token{
		kind:  kind,
		value: value,
		pos:   s.position(),
	})
}

// emitAdvance appends a single-byte token and advances past it.
func (s *scanner) emitAdvance(kind tokenKind, value string) {
	s.emit(kind, value)
	s.advance()
}

// position returns the current scanner position.
func (s *scanner) position() Position {
	return Position{Line: s.line, Column: s.col, Offset: s.pos}
}

// peek returns the byte at the current position. Caller must ensure !atEnd().
func (s *scanner) peek() byte {
	return s.src[s.pos]
}

// atEnd reports whether the scanner has consumed all input.
func (s *scanner) atEnd() bool {
	return s.pos >= len(s.src)
}

// advance moves the scanner one byte forward, updating line/column counters.
// Both '\n' and bare '\r' (without following '\n') count as line breaks for
// position purposes; CRLF is treated as a single line break by the caller.
func (s *scanner) advance() {
	if s.pos >= len(s.src) {
		return
	}
	switch s.src[s.pos] {
	case '\n':
		s.line++
		s.col = 1
	case '\r':
		// Bare CR or first byte of CRLF — count as line break. If followed
		// by LF, the LF will increment line count.
		if s.pos+1 < len(s.src) && s.src[s.pos+1] == '\n' {
			// CRLF: line increments on the LF, leave CR as same-line.
			s.col++
		} else {
			s.line++
			s.col = 1
		}
	default:
		s.col++
	}
	s.pos++
}

// syntaxError constructs a SyntaxError at the current position.
func (s *scanner) syntaxError(msg string) error {
	return &SyntaxError{Message: msg, Pos: s.position()}
}

// syntaxErrorAt constructs a SyntaxError at the given position.
func (s *scanner) syntaxErrorAt(pos Position, msg string) error {
	return &SyntaxError{Message: msg, Pos: pos}
}

// isHex reports whether c is a hexadecimal digit (case-insensitive).
func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// isLetter reports whether c is an ASCII letter.
func isLetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// isDigit reports whether c is an ASCII digit.
func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

// Valid reports whether data is a valid JSONC document. It accepts JSONC
// extensions (comments, trailing commas) by default. To validate strict
// RFC 8259 JSON, use [Parse] with [WithStrictJSON] and check the error.
func Valid(data []byte) bool {
	_, err := Parse(data)
	return err == nil
}
