package jsonc

type tokenKind int

const (
	tokenError tokenKind = iota
	tokenStreamStart
	tokenStreamEnd
	tokenObjectStart
	tokenObjectEnd
	tokenArrayStart
	tokenArrayEnd
	tokenNameSeparator
	tokenValueSeparator
	tokenString
	tokenNumber
	tokenTrue
	tokenFalse
	tokenNull
	tokenLineComment
	tokenBlockComment
	tokenNewline
	tokenEOF
)

// token is the unit produced by the scanner. value holds the raw source bytes
// of the token (for strings, including the surrounding quotes; for comments,
// the leading // or /* and trailing */ delimiters are not included).
type token struct {
	kind  tokenKind
	value string
	pos   Position
}

func (t token) String() string {
	switch t.kind {
	case tokenStreamStart:
		return "STREAM-START"
	case tokenStreamEnd:
		return "STREAM-END"
	case tokenObjectStart:
		return "OBJECT-START({)"
	case tokenObjectEnd:
		return "OBJECT-END(})"
	case tokenArrayStart:
		return "ARRAY-START([)"
	case tokenArrayEnd:
		return "ARRAY-END(])"
	case tokenNameSeparator:
		return "NAME-SEPARATOR(:)"
	case tokenValueSeparator:
		return "VALUE-SEPARATOR(,)"
	case tokenString:
		return "STRING(" + t.value + ")"
	case tokenNumber:
		return "NUMBER(" + t.value + ")"
	case tokenTrue:
		return "TRUE"
	case tokenFalse:
		return "FALSE"
	case tokenNull:
		return "NULL"
	case tokenLineComment:
		return "LINE-COMMENT(" + t.value + ")"
	case tokenBlockComment:
		return "BLOCK-COMMENT(" + t.value + ")"
	case tokenNewline:
		return "NEWLINE"
	case tokenEOF:
		return "EOF"
	case tokenError:
		return "ERROR(" + t.value + ")"
	default:
		return "UNKNOWN"
	}
}
