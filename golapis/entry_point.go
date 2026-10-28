package golapis

// EntryPoint represents a source of Lua code to execute
type EntryPoint interface {
	load(gls *GolapisLuaState) error
	run(gls *GolapisLuaState) error // execute once on the running state's loop, as the init_worker phase
	String() string                 // for logging
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

func (f FileEntryPoint) run(gls *GolapisLuaState) error {
	return gls.submit(&StateEvent{Type: EventRunFile, Filename: f.Filename, Phase: "init_worker"}).Error
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

func (c CodeEntryPoint) run(gls *GolapisLuaState) error {
	return gls.submit(&StateEvent{Type: EventRunString, Code: c.Code, Phase: "init_worker"}).Error
}

func (c CodeEntryPoint) String() string {
	return "(code string)"
}
