package format

import "testing"

func TestIsSafe(t *testing.T) {
	cases := []struct {
		path string
		safe bool
	}{
		{"com/example/app/1.0.0/app-1.0.0.jar", true},
		{"@types/node/-/node-20.1.0.tgz", true},
		{"-/package/express/dist-tags", true},
		{"", false},
		{"com//example", false},
		{"com/../etc/passwd", false},
		{"./app.jar", false},
		{"/leading", false},
		{"trailing/", false},
	}

	for _, testCase := range cases {
		t.Run(testCase.path, func(t *testing.T) {
			if got := IsSafe(testCase.path); got != testCase.safe {
				t.Fatalf("IsSafe(%q) = %v, want %v", testCase.path, got, testCase.safe)
			}
		})
	}
}
