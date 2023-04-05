package stackerr

import (
	"encoding/json"
	"fmt"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

type Stack []runtime.Frame
type Stacks []Stack

// NewStack creates a new Stack from a slice of frames.
func NewStack(frames []runtime.Frame) Stack {
	return frames
}

// NewStacks creates a new Stacks from a slice of Stack.
func NewStacks(stacks []Stack) Stacks {
	return stacks
}

// NewStacksFromFrames creates a new Stacks from a slice of slices of frames.
func NewStacksFromFrames(stacks [][]runtime.Frame) Stacks {
	stks := make([]Stack, 0, len(stacks))
	for _, s := range stacks {
		stks = append(stks, s)
	}
	return stks
}

// A regexp for parsing console stack traces
var consoleStackRegexp *regexp.Regexp = regexp.MustCompile(`(?m)^[ \t]*([^\n]+)\n[ \t]+([^\n]+):([0-9]+)[ \t]*$`)

// Format formats the stacks into a human-readable string
func (s Stacks) Format() string {
	ret := stackDivider + "\n"
	for i, stack := range s {
		ret += stack.Format() + "\n" + stackDivider
		if i != len(s)-1 {
			ret += "\n"
		}
	}
	return ret
}

func (s Stack) trimStack() Stack {
	// Trim off any final frames that are part of the runtime, not our main code
	lastFrameIdx := len(s) - 1
	for lastFrameIdx >= 0 && strings.HasPrefix(s[lastFrameIdx].Function, "runtime.") {
		lastFrameIdx--
	}
	return s[0 : lastFrameIdx+1]
}

func (s Stack) MarshalJSON() ([]byte, error) {
	type jsonFrame struct {
		Function string `json:"function"`
		File     string `json:"file"`
		Line     int    `json:"line"`
	}

	ts := s.trimStack()
	jFrames := make([]jsonFrame, len(ts))
	for i, frame := range ts {
		jFrames[i] = jsonFrame{
			Function: frame.Function,
			File:     frame.File,
			Line:     frame.Line,
		}
	}
	b, err := json.Marshal(jFrames)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// Format formats the stack into a human-readable string
func (s Stack) Format() string {
	res := ""
	ts := s.trimStack()
	for i, frame := range ts {
		res = res + fmt.Sprintf("%s\n\t%s:%d", frame.Function, frame.File, frame.Line)
		if i != len(ts)-1 {
			res += "\n"
		}
	}
	return res
}

// FormatJson formats the stack into a JSON string
func (s Stack) FormatJson() string {
	b, _ := json.Marshal(s)
	return string(b)
}

// IsParentOf checks whether the stack is a parent of the child (i.e. whether the child's stack
// trace entirely includes all frames of the parent's stack trace, and then possibly some more).
func (parent Stack) IsParentOf(child Stack) bool {
	// If the child has fewer frames than the parent, it can't really
	// be a child.
	if len(child) < len(parent) {
		return false
	}

	for offset := 1; offset <= len(parent); offset++ {
		pFrame := parent[len(parent)-offset]
		cFrame := child[len(child)-offset]
		// If the frames diverge, they are siblings/cousins, not parent/child
		if pFrame.Function != cFrame.Function || pFrame.File != cFrame.File || pFrame.Line < cFrame.Line {
			return false
		}
		// If there are more frames to check and the lines don't match, they're not parent/child
		if offset != len(parent) && pFrame.Line != cFrame.Line {
			return false
		}
	}
	return true
}

// RemoveParents removes all stacks from the set of stacks that is a parent of at least one other stack in the set of stacks.
// This ensures that there are no stacks that contain information that is included in a different stack.
func (s Stacks) RemoveParents() Stacks {
	distinctStacks := Stacks{}
	// Stacks are ordered from newest to oldest.
	// If a stack has a child stack that
	// is older, it means it was wrapped in a calling
	// function, which doesn't add any usefullness for us,
	// so don't record that stack.
	for i, stack := range s {
		hasChild := false
		for j := i + 1; j < len(s); j++ {
			if stack.IsParentOf(s[j]) {
				hasChild = true
				break
			}
		}
		if !hasChild {
			distinctStacks = append(distinctStacks, stack)
		}
	}
	return distinctStacks
}

// Distinct removes any duplicate stacks.
func (s Stacks) Distinct() Stacks {
	distinct := make(Stacks, 0, len(s))
	m := map[string]struct{}{}
	for _, stack := range s {
		k := stack.Format()
		if _, ok := m[k]; !ok {
			distinct = append(distinct, stack)
			m[k] = struct{}{}
		}
	}
	return distinct
}

// StackTrace gets the current stack
func StackTrace() Stack {
	return StackTraceWithSkippedFrames(1)
}

// StackTraceWithSkippedFrames gets the current stack, with a certain number of frames skipped.
func StackTraceWithSkippedFrames(skippedFrames int) Stack {
	s := make([]uintptr, 1024)

	// runtime.Callers + this function
	n := runtime.Callers(2+skippedFrames, s)
	s = s[:n]
	return uintptrToFrames(s)
}

func uintptrToFrames(stackPtrs []uintptr) Stack {
	f := runtime.CallersFrames(stackPtrs)
	frames := make([]runtime.Frame, 0, len(stackPtrs))
	var zeroFrame runtime.Frame

	for {
		frame, more := f.Next()
		if frame != zeroFrame {
			frames = append(frames, frame)
		}
		if !more {
			break
		}
	}

	return Stack(frames)
}

// ParseStacks parses a stack string into a Stacks struct. The input string
// can be in human-readable (console) or JSON format.
func ParseStacks(s string) Stacks {
	// Try parsing from JSON into stacks
	stacks := Stacks{}
	if err := json.Unmarshal([]byte(s), &stacks); err == nil {
		if len(stacks) > 0 && len(stacks[0]) > 0 && stacks[0][0].File != "" {
			return stacks
		}
	}

	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r", ""), "\r\n", "\n")
	// Try parsing it from console format
	blocks := strings.Split(s, "\n\n")
	for _, block := range blocks {
		matches := consoleStackRegexp.FindAllStringSubmatch(block, -1)
		if len(matches) == 0 {
			continue
		}
		stack := make(Stack, 0, len(matches))
		for _, match := range matches {
			line, _ := strconv.ParseInt(match[3], 10, 32)
			stack = append(stack, runtime.Frame{
				Function: match[1],
				File:     match[2],
				Line:     int(line),
			})
		}
		stacks = append(stacks, stack)
	}

	return stacks
}
