package core

import "testing"

func TestFailReasonOfReadsTheDecodersMessages(t *testing.T) {
	for _, c := range []struct {
		msg  string
		want FailReason
	}{
		// What a library of downloads really held.
		{`ffprobe: D:\Downloads\x\floating-ui.core.d.mts: Invalid data found when processing input`, FailDamaged},
		{`thumbnail D:\Downloads\x\checkbox@2.png: vips: VipsForeignLoad: "D:\Downloads\x\checkbox@2.png" is not a known file format`, FailDamaged},
		{`ffprobe: D:\Downloads\x\get-icon.d.mts: End of file`, FailDamaged},
		{`sample video D:\Downloads\GOAT.mp4: ffprobe: context deadline exceeded`, FailSlow},
		{`open D:\Photos\a.jpg: The process cannot access the file because it is being used by another process.`, FailLocked},
		{`open D:\Photos\a.jpg: Access is denied.`, FailLocked},
		{`embed image batch: onnxruntime: something new`, FailOther},
		{``, FailOther},
	} {
		if got := FailReasonOf(c.msg); got != c.want {
			t.Errorf("FailReasonOf(%q) = %q, want %q", c.msg, got, c.want)
		}
	}
}
