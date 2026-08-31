package provider

import "testing"

func TestEscapeBlobPath(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "model.bin", "model.bin"},
		{"nested path keeps separators", "weights/shard-1.safetensors", "weights/shard-1.safetensors"},
		{"space is escaped", "my model.bin", "my%20model.bin"},
		{"hash is escaped", "model#1.bin", "model%231.bin"},
		{"question mark is escaped", "model?.bin", "model%3F.bin"},
		{"percent is escaped", "100%.bin", "100%25.bin"},
		{"nested path with spaces", "sub dir/my model.bin", "sub%20dir/my%20model.bin"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := escapeBlobPath(testCase.in); got != testCase.want {
				t.Fatalf("escapeBlobPath(%q) = %q, want %q", testCase.in, got, testCase.want)
			}
		})
	}
}
