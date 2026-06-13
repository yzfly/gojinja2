package lexer

import (
	"sync"
	"unicode"

	"golang.org/x/text/unicode/runenames"
)

var (
	runeNameOnce sync.Once
	runeNameMap  map[string]rune
)

// lookupRuneByName 按 Unicode 字符名反查码点, 支持 \N{NAME} 转义.
// 反查表在首次使用时惰性构建 (遍历全部码点, 仅 \N 转义出现时才付出此成本).
func lookupRuneByName(name string) (rune, bool) {
	runeNameOnce.Do(func() {
		runeNameMap = make(map[string]rune, 1<<17)
		for r := rune(0); r <= unicode.MaxRune; r++ {
			if n := runenames.Name(r); n != "" && n[0] != '<' {
				runeNameMap[n] = r
			}
		}
	})
	r, ok := runeNameMap[name]
	return r, ok
}
