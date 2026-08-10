package graph

const (
	white = 0
	gray  = 1
	black = 2
)

func FindCycle(deps map[string][]string) []string {
	color := map[string]int{}
	var stack []string
	var dfs func(n string) []string
	dfs = func(n string) []string {
		color[n] = gray
		stack = append(stack, n)
		for _, m := range deps[n] {
			switch color[m] {
			case gray:
				return append(stack, m)
			case white:
				if c := dfs(m); c != nil {
					return c
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[n] = black
		return nil
	}
	for n := range deps {
		if color[n] == white {
			if c := dfs(n); c != nil {
				return c
			}
		}
	}
	return nil
}
