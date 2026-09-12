package step

import "testing"

// A bot could not read a file another bot produced: the upstream blob lives
// in nanobotd's store on the host, and the consuming container gets a
// fresh, empty store on a tmpfs. Verified against a real run before this
// existed — "read file input: open /tmp/nanobots-blobs/sha256/...: no such
// file or directory".
func TestFallbackBlobStoreReadsUpstreamButWritesLocally(t *testing.T) {
	hostDir, containerDir := t.TempDir(), t.TempDir()
	host, err := NewFSBlobStore(hostDir)
	if err != nil {
		t.Fatal(err)
	}
	scratch, err := NewFSBlobStore(containerDir)
	if err != nil {
		t.Fatal(err)
	}
	// What an earlier bot produced, now living only on the host.
	upstream, err := host.Write([]byte("<p>pineapples</p>"), "text/html")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := scratch.Read(upstream.URI); err == nil {
		t.Fatal("the plain container store should not see a host blob; the test proves nothing otherwise")
	}

	blobs := &FallbackBlobStore{Primary: scratch, Fallback: host}
	got, err := blobs.Read(upstream.URI)
	if err != nil {
		t.Fatalf("upstream blob still unreadable: %v", err)
	}
	if string(got) != "<p>pineapples</p>" {
		t.Errorf("read %q, want the upstream contents", got)
	}

	// Writes stay local — a bot must not be able to alter another run's blob.
	mine, err := blobs.Write([]byte("mine"), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := host.Read(mine.URI); err == nil {
		t.Error("a container write reached the host store; writes must stay in the container's own scratch")
	}
	if _, err := scratch.Read(mine.URI); err != nil {
		t.Errorf("a container write did not land in its own scratch: %v", err)
	}
}
