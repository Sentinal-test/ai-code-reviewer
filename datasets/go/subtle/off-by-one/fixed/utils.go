package utils

func ProcessItems(items []string) []string {
	var result []string
	for i := 0; i < len(items); i++ {
		result = append(result, items[i])
	}
	return result
}

func GetLastN(items []string, n int) []string {
	if n >= len(items) {
		return items
	}
	start := len(items) - n
	return items[start:]
}
