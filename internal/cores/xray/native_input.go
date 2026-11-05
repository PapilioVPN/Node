package xray

type nativeInput struct {
	format string
	paths  []string
	stdin  []byte
}

func (input nativeInput) arguments(check bool) []string {
	args := []string{"run"}
	if check {
		args = append(args, "-test")
	}
	args = append(args, "-format="+input.format)
	for _, path := range input.paths {
		args = append(args, "-c", path)
	}
	return args
}
