package vulnverify

// 单响应 DSL 支持：nuclei 全量模板里有 3836 个用 dsl matcher，其中 3365 个只依赖
// 同一个响应（status_code / body / header）。这些能算，就应该算；只有引用 body_2..N
// 这种多请求变量时才判「不可算」。2026-11-24 统计：471 个模板属于后者。

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type dslParser struct {
	src []rune
	pos int
	r   Response
}

// EvalDSL 计算一个单响应 DSL 表达式。
// evaluable=false 时表示表达式里有不支持的变量（如 body_2）或语法无法解析。
func EvalDSL(expr string, r Response) (value bool, evaluable bool) {
	p := &dslParser{src: []rune(expr), r: r}
	v, ok := p.parseOr()
	if !ok {
		return false, false
	}
	p.skipSpace()
	if p.pos != len(p.src) {
		return false, false
	}
	return v, true
}

func (p *dslParser) skipSpace() {
	for p.pos < len(p.src) && (p.src[p.pos] == ' ' || p.src[p.pos] == '\t' || p.src[p.pos] == '\n') {
		p.pos++
	}
}

func (p *dslParser) eat(tok string) bool {
	p.skipSpace()
	if strings.HasPrefix(string(p.src[p.pos:]), tok) {
		p.pos += len([]rune(tok))
		return true
	}
	return false
}

func (p *dslParser) parseOr() (bool, bool) {
	v, ok := p.parseAnd()
	if !ok {
		return false, false
	}
	for {
		if !p.eat("||") {
			return v, true
		}
		rhs, ok := p.parseAnd()
		if !ok {
			return false, false
		}
		v = v || rhs
	}
}

func (p *dslParser) parseAnd() (bool, bool) {
	v, ok := p.parseUnary()
	if !ok {
		return false, false
	}
	for {
		if !p.eat("&&") {
			return v, true
		}
		rhs, ok := p.parseUnary()
		if !ok {
			return false, false
		}
		v = v && rhs
	}
}

func (p *dslParser) parseUnary() (bool, bool) {
	p.skipSpace()
	if p.eat("!") {
		v, ok := p.parseUnary()
		return !v, ok
	}
	if p.eat("(") {
		v, ok := p.parseOr()
		if !ok || !p.eat(")") {
			return false, false
		}
		return v, true
	}
	return p.parseAtom()
}

func (p *dslParser) parseAtom() (bool, bool) {
	p.skipSpace()
	name := p.readIdent()
	if name == "" {
		return false, false
	}
	switch name {
	case "status_code":
		return p.cmpInt(p.r.Status)
	case "len":
		if !p.eat("(") {
			return false, false
		}
		v := p.readIdent()
		if !p.eat(")") {
			return false, false
		}
		switch v {
		case "body":
			return p.cmpInt(len(p.r.Body))
		case "all", "all_headers", "header":
			return p.cmpInt(len(p.r.Header))
		default:
			return false, false
		}
	case "contains", "icontains":
		args, ok := p.readArgs()
		if !ok || len(args) != 2 {
			return false, false
		}
		subject, ok := p.subject(args[0])
		if !ok {
			return false, false
		}
		needle := args[1]
		if name == "icontains" {
			return strings.Contains(strings.ToLower(subject), strings.ToLower(needle)), true
		}
		return strings.Contains(subject, needle), true
	case "regex":
		args, ok := p.readArgs()
		if !ok || len(args) != 2 {
			return false, false
		}
		subject, ok := p.subject(args[1])
		if !ok {
			return false, false
		}
		// nuclei 的 regex(pattern, subject) 顺序是 pattern 在前。
		re, err := regexp.Compile(args[0])
		if err != nil {
			return false, false
		}
		return re.MatchString(subject), true
	default:
		return false, false
	}
}

func (p *dslParser) subject(tok string) (string, bool) {
	switch tok {
	case "body":
		return p.r.Body, true
	case "header", "all_headers", "headers":
		return p.r.Header, true
	case "all":
		return p.r.Header + "\n\n" + p.r.Body, true
	default:
		return "", false
	}
}

func (p *dslParser) parseAtomNotUsed() {}

func (p *dslParser) cmpInt(v int) (bool, bool) {
	p.skipSpace()
	op, ok := p.readOp()
	if !ok {
		return false, false
	}
	p.skipSpace()
	n, ok := p.readInt()
	if !ok {
		return false, false
	}
	switch op {
	case "==":
		return v == n, true
	case "!=":
		return v != n, true
	case ">":
		return v > n, true
	case "<":
		return v < n, true
	case ">=":
		return v >= n, true
	case "<=":
		return v <= n, true
	default:
		return false, false
	}
}

func (p *dslParser) readOp() (string, bool) {
	for _, op := range []string{"==", "!=", ">=", "<=", ">", "<"} {
		if p.eat(op) {
			return op, true
		}
	}
	return "", false
}

func (p *dslParser) readInt() (int, bool) {
	p.skipSpace()
	start := p.pos
	for p.pos < len(p.src) && (p.src[p.pos] >= '0' && p.src[p.pos] <= '9') {
		p.pos++
	}
	if start == p.pos {
		return 0, false
	}
	n, err := strconv.Atoi(string(p.src[start:p.pos]))
	if err != nil {
		return 0, false
	}
	return n, true
}

func (p *dslParser) readIdent() string {
	p.skipSpace()
	start := p.pos
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			p.pos++
			continue
		}
		break
	}
	return string(p.src[start:p.pos])
}

// readArgs 读 (a, b)：字符串字面量去引号，其余原样（标识符）。
func (p *dslParser) readArgs() ([]string, bool) {
	if !p.eat("(") {
		return nil, false
	}
	var out []string
	for {
		p.skipSpace()
		if p.pos >= len(p.src) {
			return nil, false
		}
		if p.src[p.pos] == '"' || p.src[p.pos] == '\'' {
			q := p.src[p.pos]
			p.pos++
			start := p.pos
			for p.pos < len(p.src) && p.src[p.pos] != q {
				p.pos++
			}
			if p.pos >= len(p.src) {
				return nil, false
			}
			out = append(out, string(p.src[start:p.pos]))
			p.pos++
		} else {
			out = append(out, p.readIdent())
		}
		p.skipSpace()
		if p.eat(",") {
			continue
		}
		if p.eat(")") {
			return out, true
		}
		return nil, false
	}
}

var _ = fmt.Sprintf
