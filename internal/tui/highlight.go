package tui

import (
	"image/color"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
)

// Syntax highlight foreground colors (designed for dark terminals).
var (
	hlKeywordFg = lipgloss.Color("75")  // blue
	hlStringFg  = lipgloss.Color("179") // amber
	hlCommentFg = lipgloss.Color("242") // gray
	hlNumberFg  = lipgloss.Color("176") // magenta
	hlDefaultFg = colorWhite

	// Diff background tints
	hlBgAdd = lipgloss.Color("22") // dark green
	hlBgDel = lipgloss.Color("52") // dark red
)

type langDef struct {
	lineComment string
	keywords    map[string]bool
}

func makeKeywords(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

var languages = map[string]*langDef{
	"go": {
		lineComment: "//",
		keywords: makeKeywords(
			"break", "case", "chan", "const", "continue", "default", "defer",
			"else", "fallthrough", "for", "func", "go", "goto", "if",
			"import", "interface", "map", "package", "range", "return",
			"select", "struct", "switch", "type", "var",
			"nil", "true", "false", "iota",
			"string", "int", "int8", "int16", "int32", "int64",
			"uint", "uint8", "uint16", "uint32", "uint64",
			"float32", "float64", "complex64", "complex128",
			"bool", "byte", "rune", "error", "any", "uintptr",
			"append", "cap", "close", "copy", "delete", "len", "make", "new",
			"panic", "print", "println", "recover",
		),
	},
	"rust": {
		lineComment: "//",
		keywords: makeKeywords(
			"as", "async", "await", "break", "const", "continue", "crate",
			"dyn", "else", "enum", "extern", "fn", "for", "if", "impl",
			"in", "let", "loop", "match", "mod", "move", "mut", "pub",
			"ref", "return", "self", "Self", "static", "struct", "super",
			"trait", "type", "unsafe", "use", "where", "while", "yield",
			"true", "false", "None", "Some", "Ok", "Err",
			"i8", "i16", "i32", "i64", "i128", "isize",
			"u8", "u16", "u32", "u64", "u128", "usize",
			"f32", "f64", "bool", "char", "str",
			"String", "Vec", "Box", "Option", "Result",
			"println", "eprintln", "format", "panic", "todo", "unimplemented",
		),
	},
	"python": {
		lineComment: "#",
		keywords: makeKeywords(
			"and", "as", "assert", "async", "await", "break", "class",
			"continue", "def", "del", "elif", "else", "except", "finally",
			"for", "from", "global", "if", "import", "in", "is", "lambda",
			"nonlocal", "not", "or", "pass", "raise", "return", "try",
			"while", "with", "yield",
			"True", "False", "None",
			"int", "float", "str", "bool", "list", "dict", "tuple", "set",
			"print", "len", "range", "type", "isinstance", "super", "self",
		),
	},
	"javascript": {
		lineComment: "//",
		keywords: makeKeywords(
			"async", "await", "break", "case", "catch", "class", "const",
			"continue", "debugger", "default", "delete", "do", "else",
			"export", "extends", "finally", "for", "function", "if",
			"import", "in", "instanceof", "let", "new", "of", "return",
			"static", "super", "switch", "this", "throw", "try", "typeof",
			"var", "void", "while", "with", "yield",
			"true", "false", "null", "undefined", "NaN", "Infinity",
			"console", "require", "module", "exports",
		),
	},
	"typescript": {
		lineComment: "//",
		keywords: makeKeywords(
			"abstract", "as", "async", "await", "break", "case", "catch",
			"class", "const", "continue", "debugger", "declare", "default",
			"delete", "do", "else", "enum", "export", "extends", "finally",
			"for", "from", "function", "if", "implements", "import", "in",
			"instanceof", "interface", "is", "keyof", "let", "namespace",
			"new", "of", "override", "readonly", "return", "satisfies",
			"static", "super", "switch", "this", "throw", "try", "type",
			"typeof", "var", "void", "while", "with", "yield",
			"true", "false", "null", "undefined", "NaN",
			"string", "number", "boolean", "any", "never", "unknown",
			"object", "symbol", "bigint", "void",
			"Array", "Promise", "Record", "Partial", "Required", "Readonly",
			"console", "require",
		),
	},
	"shell": {
		lineComment: "#",
		keywords: makeKeywords(
			"if", "then", "else", "elif", "fi", "for", "while", "do",
			"done", "case", "esac", "in", "function", "return", "exit",
			"export", "local", "readonly", "declare", "typeset", "unset",
			"shift", "source", "eval", "exec", "trap", "set",
			"true", "false",
			"echo", "printf", "read", "test", "cd", "pwd",
		),
	},
	"sql": {
		lineComment: "--",
		keywords: makeKeywords(
			"SELECT", "FROM", "WHERE", "INSERT", "INTO", "UPDATE", "DELETE",
			"CREATE", "ALTER", "DROP", "TABLE", "INDEX", "VIEW", "DATABASE",
			"JOIN", "LEFT", "RIGHT", "INNER", "OUTER", "CROSS", "ON",
			"AND", "OR", "NOT", "IN", "IS", "NULL", "LIKE", "BETWEEN",
			"ORDER", "BY", "GROUP", "HAVING", "LIMIT", "OFFSET", "AS",
			"SET", "VALUES", "DEFAULT", "PRIMARY", "KEY", "FOREIGN",
			"REFERENCES", "UNIQUE", "CHECK", "CONSTRAINT", "CASCADE",
			"EXISTS", "DISTINCT", "UNION", "ALL", "ANY", "SOME",
			"BEGIN", "COMMIT", "ROLLBACK", "TRANSACTION",
			"INTEGER", "TEXT", "REAL", "BLOB", "BOOLEAN", "VARCHAR",
			"IF", "THEN", "ELSE", "END", "CASE", "WHEN",
			// lowercase variants
			"select", "from", "where", "insert", "into", "update", "delete",
			"create", "alter", "drop", "table", "index", "view", "database",
			"join", "left", "right", "inner", "outer", "cross", "on",
			"and", "or", "not", "in", "is", "null", "like", "between",
			"order", "by", "group", "having", "limit", "offset", "as",
			"set", "values", "default", "primary", "key", "foreign",
			"references", "unique", "check", "constraint", "cascade",
			"exists", "distinct", "union", "all", "any", "some",
			"begin", "commit", "rollback", "transaction",
			"integer", "text", "real", "blob", "boolean", "varchar",
			"if", "then", "else", "end", "case", "when",
		),
	},
	"ruby": {
		lineComment: "#",
		keywords: makeKeywords(
			"BEGIN", "END", "alias", "and", "begin", "break", "case",
			"class", "def", "defined?", "do", "else", "elsif", "end",
			"ensure", "for", "if", "in", "module", "next", "nil", "not",
			"or", "redo", "rescue", "retry", "return", "self", "super",
			"then", "unless", "until", "when", "while", "yield",
			"true", "false", "nil",
			"require", "include", "extend", "attr_accessor", "attr_reader",
			"puts", "print", "raise",
		),
	},
	"java": {
		lineComment: "//",
		keywords: makeKeywords(
			"abstract", "assert", "boolean", "break", "byte", "case",
			"catch", "char", "class", "continue", "default", "do",
			"double", "else", "enum", "extends", "final", "finally",
			"float", "for", "if", "implements", "import", "instanceof",
			"int", "interface", "long", "native", "new", "package",
			"private", "protected", "public", "return", "short", "static",
			"strictfp", "super", "switch", "synchronized", "this", "throw",
			"throws", "transient", "try", "void", "volatile", "while",
			"true", "false", "null",
			"var", "record", "sealed", "permits", "yield",
			"String", "Integer", "Long", "Double", "Float", "Boolean",
			"System", "Override",
		),
	},
	"c": {
		lineComment: "//",
		keywords: makeKeywords(
			"auto", "break", "case", "char", "const", "continue", "default",
			"do", "double", "else", "enum", "extern", "float", "for",
			"goto", "if", "inline", "int", "long", "register", "return",
			"short", "signed", "sizeof", "static", "struct", "switch",
			"typedef", "union", "unsigned", "void", "volatile", "while",
			"NULL", "true", "false",
			"int8_t", "int16_t", "int32_t", "int64_t",
			"uint8_t", "uint16_t", "uint32_t", "uint64_t",
			"size_t", "ssize_t", "bool",
			"printf", "fprintf", "sprintf", "malloc", "free", "sizeof",
		),
	},
}

var extToLang = map[string]string{
	".go":    "go",
	".rs":    "rust",
	".py":    "python",
	".pyw":   "python",
	".js":    "javascript",
	".mjs":   "javascript",
	".cjs":   "javascript",
	".jsx":   "javascript",
	".ts":    "typescript",
	".tsx":   "typescript",
	".mts":   "typescript",
	".sh":    "shell",
	".bash":  "shell",
	".zsh":   "shell",
	".sql":   "sql",
	".rb":    "ruby",
	".java":  "java",
	".c":     "c",
	".h":     "c",
	".cpp":   "c",
	".cc":    "c",
	".hpp":   "c",
	".cs":    "java",
	".kt":    "java",
	".swift": "java",
}

// langFromPath returns the language identifier for a file path,
// or "" if the extension is not recognized.
func langFromPath(path string) string {
	ext := filepath.Ext(path)
	return extToLang[strings.ToLower(ext)]
}

// highlightLine applies syntax coloring to a single line of code.
// An optional background color can be passed to tint the entire line
// (used for diff addition/deletion backgrounds).
// Returns the original line unchanged if lang is empty or unrecognized.
func highlightLine(line, lang string, bg ...color.Color) string {
	if line == "" {
		return ""
	}

	style := func(fg color.Color) lipgloss.Style {
		s := lipgloss.NewStyle().Foreground(fg)
		if len(bg) > 0 {
			s = s.Background(bg[0])
		}
		return s
	}

	if lang == "" {
		return style(hlDefaultFg).Render(line)
	}
	def, ok := languages[lang]
	if !ok {
		return style(hlDefaultFg).Render(line)
	}

	var b strings.Builder
	i := 0
	n := len(line)

	for i < n {
		// line comment: rest of line is a comment
		if def.lineComment != "" && i+len(def.lineComment) <= n &&
			line[i:i+len(def.lineComment)] == def.lineComment {
			b.WriteString(style(hlCommentFg).Render(line[i:]))
			return b.String()
		}

		// string literal
		if line[i] == '"' || line[i] == '\'' || line[i] == '`' {
			quote := line[i]
			j := i + 1
			for j < n {
				if line[j] == '\\' && quote != '`' {
					j += 2
					continue
				}
				if line[j] == quote {
					j++
					break
				}
				j++
			}
			b.WriteString(style(hlStringFg).Render(line[i:j]))
			i = j
			continue
		}

		// number (preceded by non-identifier char or start of line)
		if isDigit(line[i]) && (i == 0 || !isIdentChar(line[i-1])) {
			j := i
			for j < n && (isDigit(line[j]) || line[j] == '.' ||
				line[j] == 'x' || line[j] == 'X' ||
				line[j] == 'o' || line[j] == 'b' ||
				line[j] == '_' ||
				(line[j] >= 'a' && line[j] <= 'f') ||
				(line[j] >= 'A' && line[j] <= 'F')) {
				j++
			}
			b.WriteString(style(hlNumberFg).Render(line[i:j]))
			i = j
			continue
		}

		// identifier or keyword
		if isIdentStart(line[i]) {
			j := i
			for j < n && isIdentChar(line[j]) {
				j++
			}
			word := line[i:j]
			if def.keywords[word] {
				b.WriteString(style(hlKeywordFg).Render(word))
			} else {
				b.WriteString(style(hlDefaultFg).Render(word))
			}
			i = j
			continue
		}

		// default: single character (operators, punctuation, whitespace)
		b.WriteString(style(hlDefaultFg).Render(string(line[i])))
		i++
	}

	return b.String()
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func isIdentStart(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
}

func isIdentChar(c byte) bool {
	return isIdentStart(c) || isDigit(c)
}
