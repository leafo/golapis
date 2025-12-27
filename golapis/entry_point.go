package golapis

// EntryPoint represents a source of Lua code to execute
type EntryPoint interface {
	load(gls *GolapisLuaState) error
	String() string // for logging
}

// FileEntryPoint loads Lua code from a file
type FileEntryPoint struct {
	Filename string
}

func (f FileEntryPoint) load(gls *GolapisLuaState) error {
	if err := gls.loadFile(f.Filename); err != nil {
		return err
	}
	gls.storeEntryPoint()
	return nil
}

func (f FileEntryPoint) String() string {
	return f.Filename
}

// CodeEntryPoint loads Lua code from a string
type CodeEntryPoint struct {
	Code string
}

func (c CodeEntryPoint) load(gls *GolapisLuaState) error {
	if err := gls.loadString(c.Code); err != nil {
		return err
	}
	gls.storeEntryPoint()
	return nil
}

func (c CodeEntryPoint) String() string {
	return "(code string)"
}
