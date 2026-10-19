package service

import "testing"

func TestLegacyOwnerProgramJSONPreservesGoldenBytes(t *testing.T) {
	if got := legacyOwnerProgramJSON("windows", "S-1-test", `C:\OA\runtime.exe`); got != `["owner-program@1","windows","S-1-test","C:\\OA\\runtime.exe"]` {
		t.Fatal(got)
	}
}
