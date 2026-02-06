package logic

// CalculateSum sums all integers in the slice.
func CalculateSum(values []int) int {
	sum := 0
	for i := 0; i < len(values); i++ {
		sum += values[i]
	}
	return sum
}

// FindMax finds the maximum value in the slice.
func FindMax(values []int) int {
	if len(values) == 0 {
		return 0
	}
	max := values[0]
	for i := 1; i < len(values); i++ {
		if values[i] > max {
			max = values[i]
		}
	}
	return max
}
