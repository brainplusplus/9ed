package chat

import (
	"testing"
)

func TestPtyRingBufferWriteAndSnapshot(t *testing.T) {
	rb := newPtyRingBuffer(1024)

	// Write some data.
	rb.Write([]byte("hello world"))

	snap := rb.Snapshot()
	if string(snap) != "hello world" {
		t.Fatalf("Expected 'hello world', got %q", string(snap))
	}
}

func TestPtyRingBufferMultipleWrites(t *testing.T) {
	rb := newPtyRingBuffer(1024)

	rb.Write([]byte("aaa"))
	rb.Write([]byte("bbb"))
	rb.Write([]byte("ccc"))

	snap := rb.Snapshot()
	if string(snap) != "aaabbbccc" {
		t.Fatalf("Expected 'aaabbbccc', got %q", string(snap))
	}
}

func TestPtyRingBufferEviction(t *testing.T) {
	// Small buffer to test eviction.
	rb := newPtyRingBuffer(10)

	// Write more than the buffer size.
	rb.Write([]byte("0123456789ABCDEF"))

	snap := rb.Snapshot()
	if len(snap) != 10 {
		t.Fatalf("Expected 10 bytes after eviction, got %d", len(snap))
	}
	// Should contain the last 10 bytes: "6789ABCDEF"
	if string(snap) != "6789ABCDEF" {
		t.Fatalf("Expected '6789ABCDEF', got %q", string(snap))
	}
}

func TestPtyRingBufferEmptySnapshot(t *testing.T) {
	rb := newPtyRingBuffer(1024)
	snap := rb.Snapshot()
	if len(snap) != 0 {
		t.Fatalf("Expected empty snapshot, got %d bytes", len(snap))
	}
}

func TestPtyRingBufferWraparound(t *testing.T) {
	// Buffer of size 10, write in chunks that cause wraparound.
	rb := newPtyRingBuffer(10)

	// Fill to 8.
	rb.Write([]byte("01234567"))
	// Write 5 more, causing eviction and wraparound.
	rb.Write([]byte("ABCDE"))

	snap := rb.Snapshot()
	// Should contain last 10 bytes: "234567ABCD" wait, let's trace:
	// After "01234567": total=8, start=0, data=[01234567....]
	// Write "ABCDE" (5 bytes): avail=10-8=2, evict 5, start=(0+5)%10=5, total=8-5=3
	// avail=5. writeLen=5. endPos=(5+3)%10=8. endAvail=10-8=2.
	// copy(data[8:], "AB") -> data=[01234567AB]
	// copy(data[0:], "CDE") -> data=[CDE34567AB]
	// total=3+5=8.
	// Snapshot: start=5, total=8, 8 < 10, so copy(out, data[5:5+8])
	// data[5:13] = data[5:10] + data[0:3] = "567AB" + "CDE" = "567ABCDE"? Wait.
	// Actually data = [C,D,E,3,4,5,6,7,A,B] (indices 0-9)
	// start=5, total=8.
	// total < size, so: copy(out, data[5:5+8]) = data[5:13] = data[5:10] + data[10:13]
	// But data[10:13] is out of bounds for the snapshot. The code handles this:
	// if total < size, copy(out, data[start:start+total])
	// data[5:13] -> data[5], data[6], data[7], data[8], data[9], data[10](panic?)
	// Actually, the code does: copy(out, rb.data[rb.start:rb.start+rb.total])
	// Which is data[5:13], but since rb.data has length 10, this would be
	// data[5:10] only (slicing clamps). That's a bug! Let me check.
	// Actually, Go slicing: data[5:13] on a slice of len 10 gives data[5:10] (5 elements).
	// So the snapshot would be "567AB" (5 bytes) instead of 8.
	// This is actually correct behavior because the ring buffer code
	// uses the wraparound copy in Write. Let me trace more carefully.
	// After the first Write("01234567"): data=[0,1,2,3,4,5,6,7,_,_], start=0, total=8
	// Write("ABCDE"): avail=10-8=2. Since avail != 0, evict len("ABCDE")=5.
	// But wait, avail is 2, but we need to write 5. So writeLen=2 initially?
	// No, the evict logic: avail=0 triggers eviction, but here avail=2.
	// So the code goes: avail=2, writeLen=5, writeLen>avail, so writeLen=2.
	// Then it writes 2 bytes: endPos=(0+8)%10=8, endAvail=10-8=2.
	// copy(data[8:10], "AB") -> data=[0,1,2,3,4,5,6,7,A,B]
	// total=8+2=10, p="CDE" (remaining 3 bytes).
	// Next iteration: avail=10-10=0. Evict len("CDE")=3.
	// start=(0+3)%10=3, total=10-3=7. avail=3.
	// writeLen=3. endPos=(3+7)%10=0. endAvail=10-0=10. 3<=10.
	// copy(data[0:3], "CDE") -> data=[C,D,E,3,4,5,6,7,A,B]
	// total=7+3=10.
	// Snapshot: total=10 >= size=10.
	// firstLen=10-3=7. copy(out[:7], data[3:]) = "3,4,5,6,7,A,B" = "34567AB"
	// copy(out[7:], data[:10-7]) = data[:3] = "C,D,E" = "CDE"
	// Result: "34567ABCDE"
	if string(snap) != "34567ABCDE" {
		t.Fatalf("Expected '34567ABCDE', got %q", string(snap))
	}
}
