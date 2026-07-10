package connutil

type fakeAddr struct{}

func (fakeAddr) Network() string { return "belljar" }
func (fakeAddr) String() string  { return "wymark" }
