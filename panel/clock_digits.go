package main

func clockDigits(f []byte, h, m, s int, stack bool, offset int) {
	put := func(x, y int, v byte) {
		y += offset
		if x >= 0 && x < 25 && y >= 0 && y < 16 {
			if v > f[y*25+x] {
				f[y*25+x] = v
			}
		}
	}
	if stack {
		for row, n := range []int{h, m} {
			for col, d := range []int{n / 10, n % 10} {
				for y, line := range digitGlyphs[d] {
					for x, c := range line {
						if c == '1' {
							put(7+col*6+x, 1+row*8+y, 255)
						}
					}
				}
			}
		}
		for x := 4; x < 21; x++ {
			put(x, 7, 40)
		}
		return
	}
	segments := []string{"abcdef", "bc", "abdeg", "abcdg", "bcfg", "acdfg", "acdefg", "abc", "abcdefg", "abcdfg"}
	for col, n := range []int{h / 10, h % 10, m / 10, m % 10} {
		x := col * 6
		if col > 1 {
			x++
		}
		on := segments[n]
		for _, seg := range on {
			switch seg {
			case 'a', 'g', 'd':
				y := 1
				if seg == 'g' {
					y = 7
				}
				if seg == 'd' {
					y = 13
				}
				for dx := 1; dx <= 3; dx++ {
					put(x+dx, y, 255)
				}
			case 'b', 'c', 'e', 'f':
				dx, y := 4, 2
				if seg == 'c' || seg == 'e' {
					y = 8
				}
				if seg == 'e' || seg == 'f' {
					dx = 0
				}
				for dy := 0; dy < 5; dy++ {
					put(x+dx, y+dy, 255)
				}
			}
		}
	}
	if s%2 == 0 {
		put(12, 5, 100)
		put(12, 10, 100)
	}
}
