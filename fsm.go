// Edge Network
// (c) 2019 Edge Network technologies Ltd.

package fsm

import (
	"context"
	"fmt"
)

// StateMachine is the finite state machine struct.
type StateMachine struct {
	CurrentState *State
	States       []*State
	transitions  chan *Transition
	// beforeFn runs before the state change.
	beforeFn func(*Transition)
	// afterFn runs after the state is change.
	afterFn func(*Transition)

	initialized bool
	ctx         context.Context
	cancel      context.CancelFunc
}

// State contains state configuration.
type State struct {
	Source      []string
	Destination string

	// onEnterFunc is the function called when the state is entered.
	onEnterFunc func(*State)

	// parallel decides whnever the onEnterFunc should be called in a new goroutine.
	parallel bool
	fromAny  bool
	ctx      context.Context
	cancel   context.CancelFunc
}

// Transition contains transition information.
type Transition struct {
	From *State
	To   *State
}

// To assigns a Destination to the State.
func (st *State) To(dn string) *State {
	st.Destination = dn
	return st
}

// FromAny allows the state to be transitioned to from any other state.
func (st *State) FromAny() *State {
	st.fromAny = true
	return st
}

// From assigns a Source to the State.
func (st *State) From(src ...string) *State {
	st.Source = src
	return st
}

// Transitions returns the transition channels.
func (s *StateMachine) Transitions() <-chan *Transition {
	return s.transitions
}

// BeforeTransition sets an action to be called before state transition is executed.
func (s *StateMachine) BeforeTransition(f func(*Transition)) {
	// Store the method.
	s.beforeFn = f
}

// AfterTransition sets an action to be called after state transition is executed.
func (s *StateMachine) AfterTransition(f func(*Transition)) {
	// Store the method.
	s.afterFn = f
}

// OnEnter setups the function to be called when a state is entered.
func (st *State) OnEnter(f func(s *State)) *State {
	st.onEnterFunc = f
	return st
}

// Parallel sets how the onEnterFunc should be called.
func (st *State) Parallel(p bool) *State {
	st.parallel = p
	return st
}

// Context returns the states context.
func (st *State) Context() context.Context {
	if st.ctx != nil {
		return st.ctx
	}

	return context.Background()
}

// do executes the transition by exiting the previous state, and entering the new one.
func (t *Transition) do() {
	if t.To.onEnterFunc != nil {
		t.To.onEnterFunc(t.To)
	}
}

func (s *StateMachine) before(t *Transition) {
	if s.beforeFn != nil {
		s.beforeFn(t)
	}
}

func (s *StateMachine) after(t *Transition) {
	if s.afterFn != nil {
		s.afterFn(t)
	}
}

// Find locates a state by name.
func (s *StateMachine) Find(st string) (state *State, err error) {
	for _, state := range s.States {
		if state.Destination == st {
			return state, nil
		}
	}

	return nil, fmt.Errorf("Invalid state: %v", st)
}

// Match returns true when the input matches the current state Destination.
func (s *StateMachine) Match(compare ...string) bool {
	if !s.Exists() {
		return false
	}

	for _, state := range compare {
		match := s.CurrentState.Destination == state
		if match {
			return true
		}
	}
	return false
}

// Exists determines whether a state has been set.
func (s *StateMachine) Exists() bool {
	return s.CurrentState != nil
}

// Start launches the state machine
func (s *StateMachine) Start() {
	if s.initialized {
		return
	}

	s.initialized = true

	go func() {
		for {
			select {
			case <-s.ctx.Done():
				return
			case t := <-s.transitions:
				if s.ctx.Err() != nil {
					return
				}

				s.before(t)
				t.do()
				s.after(t)
			}
		}
	}()
}

// Name returns the current States destination name.
func (s *StateMachine) Name() string {
	if s.Exists() {
		return s.CurrentState.Destination
	}
	return ""
}

// IsValidStateChange returns an error when the state change is not permitted.
func (s *StateMachine) IsValidStateChange(name string) (*State, error) {
	// Find next state
	st, err := s.Find(name)
	if err != nil {
		return st, err
	}

	// This state accepts transitions from any other state.
	if st.fromAny {
		return st, nil
	}

	// There is no existing origin state so any entrypoint is allowed.
	if s.CurrentState == nil {
		return st, nil
	}

	for _, source := range st.Source {
		if source == s.CurrentState.Destination {
			return st, nil
		}
	}

	return st, fmt.Errorf("Invalid state change: %v > %v", s.CurrentState.Destination, st.Destination)
}

// Transition changes the state when permissible.
func (s *StateMachine) Transition(to string) (err error) {
	// Ignore transitions to the same state.
	if s.Match(to) {
		return
	}

	// Check if new state is valid.
	state, err := s.IsValidStateChange(to)

	if err != nil {
		return
	}

	// Give the inbound state a new context.
	if s.ctx != nil {
		state.ctx, state.cancel = context.WithCancel(s.ctx)
	}

	// Send transition to channel
	tr := &Transition{
		From: s.CurrentState,
		To:   state,
	}

	// Cancel current state context.
	if s.CurrentState != nil && s.CurrentState.cancel != nil {
		s.CurrentState.cancel()
	}

	if state.parallel {
		go tr.do()
	} else {
		if s.ctx != nil && s.ctx.Err() != nil {
			return
		}
		s.transitions <- tr
	}
	s.CurrentState = state
	return
}

// NewState returns a new state instance.
func (s *StateMachine) NewState() *State {
	st := &State{}
	s.States = append(s.States, st)

	return st
}

// WithContext applies a context to the state machine.
func (s *StateMachine) WithContext(ctx context.Context) *StateMachine {
	s.ctx, s.cancel = context.WithCancel(ctx)
	return s
}

// New returns a new, empty StateMachine instance
func New() *StateMachine {
	return &StateMachine{
		transitions: make(chan *Transition, 1),
	}
}
