package progress

type Sink interface {
	Start(total int64, label string)
	Add(n int64)
	Done()
}

type Resumer interface{ Resume(done int64) }

type Counter interface{ Counting(noun string) }

func Counting(s Sink, noun string) {
	if c, ok := s.(Counter); ok {
		c.Counting(noun)
	}
}

func Resume(s Sink, done int64) {
	if r, ok := s.(Resumer); ok {
		r.Resume(done)
	}
}

type Nop struct{}

func (Nop) Start(int64, string) {}
func (Nop) Add(int64)           {}
func (Nop) Counting(string)     {}
func (Nop) Done()               {}
func (Nop) Resume(int64)        {}

func Of(s Sink) Sink {
	if s == nil {
		return Nop{}
	}
	return s
}

type within struct{ s Sink }

func Within(s Sink) Sink { return within{s: Of(s)} }

func (within) Start(int64, string) {}
func (w within) Add(n int64)       { w.s.Add(n) }
func (within) Done()               {}
