package lua

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestGoToLuaNil(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	got := goToLua(L, nil)
	if got != lua.LNil {
		t.Errorf("goToLua(nil) = %v, want lua.LNil", got)
	}
}

func TestGoToLuaBool(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	tests := []bool{true, false}
	for _, val := range tests {
		got := goToLua(L, val)
		if lua.LVAsBool(got) != val {
			t.Errorf("goToLua(%v) = %v, wrong bool value", val, got)
		}
	}
}

func TestGoToLuaNumber(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	tests := []float64{0, 1, -1, 3.14, 1e10}
	for _, val := range tests {
		got := goToLua(L, val)
		f := float64(got.(lua.LNumber))
		if f != val {
			t.Errorf("goToLua(%v) = %v, want %v", val, f, val)
		}
	}
}

func TestGoToLuaString(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	tests := []string{"", "hello", "with spaces", "special!@#$"}
	for _, val := range tests {
		got := goToLua(L, val)
		s := string(got.(lua.LString))
		if s != val {
			t.Errorf("goToLua(%q) = %q, want %q", val, s, val)
		}
	}
}

func TestGoToLuaArray(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	arr := []interface{}{1.0, "two", 3.0}
	got := goToLua(L, arr)

	tbl, ok := got.(*lua.LTable)
	if !ok {
		t.Fatalf("goToLua(array) = %T, want *lua.LTable", got)
	}

	// Lua arrays are 1-indexed
	if maxN := tbl.MaxN(); maxN != 3 {
		t.Errorf("table length = %d, want 3", maxN)
	}
}

func TestGoToLuaMap(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	m := map[string]interface{}{
		"key1": "value1",
		"key2": 42.0, // Use float64 explicitly
	}
	got := goToLua(L, m)

	tbl, ok := got.(*lua.LTable)
	if !ok {
		t.Fatalf("goToLua(map) = %T, want *lua.LTable", got)
	}

	v1 := tbl.RawGetString("key1")
	if string(v1.(lua.LString)) != "value1" {
		t.Errorf("map key1 = %v, want value1", v1)
	}

	v2 := tbl.RawGetString("key2")
	if float64(v2.(lua.LNumber)) != 42 {
		t.Errorf("map key2 = %v, want 42", v2)
	}
}

func TestGoToLuaNestedStructure(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	data := map[string]interface{}{
		"array": []interface{}{1, 2},
		"nested": map[string]interface{}{
			"key": "value",
		},
	}
	got := goToLua(L, data)

	tbl, ok := got.(*lua.LTable)
	if !ok {
		t.Fatalf("goToLua returned %T, want *lua.LTable", got)
	}

	// Verify we can access nested values
	nested := tbl.RawGetString("nested")
	if nested == lua.LNil {
		t.Errorf("nested map not found")
	}
}

func TestLuaToGoNil(t *testing.T) {
	got := luaToGo(lua.LNil)
	if got != nil {
		t.Errorf("luaToGo(lua.LNil) = %v, want nil", got)
	}
}

func TestLuaToGoBool(t *testing.T) {
	tests := []bool{true, false}
	for _, val := range tests {
		got := luaToGo(lua.LBool(val))
		if got != val {
			t.Errorf("luaToGo(LBool(%v)) = %v, want %v", val, got, val)
		}
	}
}

func TestLuaToGoNumber(t *testing.T) {
	tests := []struct {
		val      float64
		wantType string
		wantVal  interface{}
	}{
		{0, "int64", int64(0)},
		{1, "int64", int64(1)},
		{-1, "int64", int64(-1)},
		{3.14, "float64", 3.14},
	}
	for _, tt := range tests {
		got := luaToGo(lua.LNumber(tt.val))
		switch v := got.(type) {
		case int64:
			if v != tt.wantVal {
				t.Errorf("luaToGo(LNumber(%v)) = %v, want %v", tt.val, v, tt.wantVal)
			}
		case float64:
			if v != tt.wantVal {
				t.Errorf("luaToGo(LNumber(%v)) = %v, want %v", tt.val, v, tt.wantVal)
			}
		default:
			t.Errorf("luaToGo(LNumber(%v)) = %T, want int64 or float64", tt.val, got)
		}
	}
}

func TestLuaToGoIntNumber(t *testing.T) {
	// When a number is a whole integer, it should return int64
	got := luaToGo(lua.LNumber(42))
	v, ok := got.(int64)
	if !ok {
		t.Errorf("luaToGo(LNumber(42)) = %T, want int64", got)
	}
	if v != 42 {
		t.Errorf("luaToGo(LNumber(42)) = %v, want 42", v)
	}
}

func TestLuaToGoString(t *testing.T) {
	tests := []string{"", "hello", "with spaces"}
	for _, val := range tests {
		got := luaToGo(lua.LString(val))
		s, ok := got.(string)
		if !ok {
			t.Errorf("luaToGo(LString(%q)) = %T, want string", val, got)
		}
		if s != val {
			t.Errorf("luaToGo(LString(%q)) = %q, want %q", val, s, val)
		}
	}
}

func TestLuaToGoArray(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	tbl := L.NewTable()
	tbl.RawSetInt(1, lua.LString("first"))
	tbl.RawSetInt(2, lua.LNumber(2))
	tbl.RawSetInt(3, lua.LBool(true))

	got := luaToGo(tbl)
	arr, ok := got.([]interface{})
	if !ok {
		t.Fatalf("luaToGo(table) = %T, want []interface{}", got)
	}

	if len(arr) != 3 {
		t.Errorf("array length = %d, want 3", len(arr))
	}
	if arr[0] != "first" || arr[1] != int64(2) || arr[2] != true {
		t.Errorf("array values incorrect: %v", arr)
	}
}

func TestLuaToGoMap(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	tbl := L.NewTable()
	tbl.RawSetString("key1", lua.LString("value1"))
	tbl.RawSetString("key2", lua.LNumber(42))

	got := luaToGo(tbl)
	m, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("luaToGo(table) = %T, want map[string]interface{}", got)
	}

	if v, ok := m["key1"]; !ok || v != "value1" {
		t.Errorf("map key1 = %v, want value1", v)
	}
	if v, ok := m["key2"]; !ok || v != int64(42) {
		t.Errorf("map key2 = %v, want 42", v)
	}
}

func TestTStrEmpty(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	tbl := L.NewTable()
	got := tStr(tbl, "missing")
	if got != "" {
		t.Errorf("tStr(missing) = %q, want empty string", got)
	}
}

func TestTStrPresent(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	tbl := L.NewTable()
	tbl.RawSetString("key", lua.LString("value"))
	got := tStr(tbl, "key")
	if got != "value" {
		t.Errorf("tStr(key) = %q, want value", got)
	}
}

func TestTStrConvertsToString(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	tbl := L.NewTable()
	tbl.RawSetString("num", lua.LNumber(42))
	got := tStr(tbl, "num")
	if got != "42" {
		t.Errorf("tStr(num) = %q, want 42", got)
	}
}

func TestTStrAltFirstKey(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	tbl := L.NewTable()
	tbl.RawSetString("key1", lua.LString("value1"))
	tbl.RawSetString("key2", lua.LString("value2"))

	got := tStrAlt(tbl, "key1", "key2")
	if got != "value1" {
		t.Errorf("tStrAlt() = %q, want value1", got)
	}
}

func TestTStrAltSecondKey(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	tbl := L.NewTable()
	tbl.RawSetString("key2", lua.LString("value2"))

	got := tStrAlt(tbl, "key1", "key2")
	if got != "value2" {
		t.Errorf("tStrAlt() = %q, want value2", got)
	}
}

func TestTStrAltNoKeys(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	tbl := L.NewTable()
	got := tStrAlt(tbl, "key1", "key2")
	if got != "" {
		t.Errorf("tStrAlt(missing) = %q, want empty string", got)
	}
}

func TestTStrAltSkipsEmptyValues(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	tbl := L.NewTable()
	tbl.RawSetString("key1", lua.LString(""))
	tbl.RawSetString("key2", lua.LString("value2"))

	got := tStrAlt(tbl, "key1", "key2")
	if got != "value2" {
		t.Errorf("tStrAlt() = %q, want value2 (skips empty key1)", got)
	}
}

func TestRunLocalSimpleCommand(t *testing.T) {
	stdout, code, errMsg := runLocal("echo hello")
	if code != 0 {
		t.Errorf("runLocal(echo hello) exit code = %d, want 0, err = %q", code, errMsg)
	}
	if stdout != "hello" {
		t.Errorf("runLocal(echo hello) stdout = %q, want hello", stdout)
	}
	if errMsg != "" {
		t.Errorf("runLocal(echo hello) errMsg = %q, want empty", errMsg)
	}
}

func TestRunLocalFailingCommand(t *testing.T) {
	_, code, errMsg := runLocal("false")
	if code != 1 {
		t.Errorf("runLocal(false) exit code = %d, want 1", code)
	}
	if errMsg != "" {
		t.Errorf("runLocal(false) errMsg = %q, want empty", errMsg)
	}
}

func TestRunLocalInvalidCommand(t *testing.T) {
	_, code, errMsg := runLocal("commandthatdoesnotexist123")
	// When a command doesn't exist, the shell exits with status 127
	if code != 127 {
		t.Errorf("runLocal(invalid) exit code = %d, want 127", code)
	}
	// Since it's an ExitError (not a connection error), errMsg should be empty
	if errMsg != "" {
		t.Errorf("runLocal(invalid) errMsg = %q, want empty", errMsg)
	}
}

func TestRunLocalMultilineOutput(t *testing.T) {
	stdout, code, errMsg := runLocal("printf 'line1\\nline2\\nline3'")
	if code != 0 {
		t.Errorf("runLocal exit code = %d, want 0", code)
	}
	expected := "line1\nline2\nline3"
	if stdout != expected {
		t.Errorf("runLocal() stdout = %q, want %q", stdout, expected)
	}
	if errMsg != "" {
		t.Errorf("runLocal() errMsg = %q, want empty", errMsg)
	}
}

func TestRunLocalTrimsTrailingNewline(t *testing.T) {
	stdout, code, errMsg := runLocal("echo hello")
	if code != 0 {
		t.Errorf("runLocal exit code = %d, want 0", code)
	}
	if stdout != "hello" {
		t.Errorf("runLocal() stdout = %q, want hello (no trailing newline)", stdout)
	}
	if errMsg != "" {
		t.Errorf("runLocal() errMsg = %q, want empty", errMsg)
	}
}

func TestRunLocalEmptyOutput(t *testing.T) {
	stdout, code, errMsg := runLocal("true")
	if code != 0 {
		t.Errorf("runLocal(true) exit code = %d, want 0", code)
	}
	if stdout != "" {
		t.Errorf("runLocal(true) stdout = %q, want empty", stdout)
	}
	if errMsg != "" {
		t.Errorf("runLocal(true) errMsg = %q, want empty", errMsg)
	}
}

func TestRunLocalEnvironmentVar(t *testing.T) {
	stdout, code, errMsg := runLocal("echo $HOME")
	if code != 0 {
		t.Errorf("runLocal exit code = %d, want 0", code)
	}
	if stdout == "" {
		t.Errorf("runLocal(echo $HOME) stdout = %q, want non-empty (but got empty)", stdout)
	}
	if errMsg != "" {
		t.Errorf("runLocal() errMsg = %q, want empty", errMsg)
	}
}

func TestGoToLuaAndLuaToGoRoundTrip(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	tests := []interface{}{
		nil,
		true,
		false,
		42.0,
		"hello",
		[]interface{}{1.0, "two", true},
	}

	for _, original := range tests {
		luaVal := goToLua(L, original)
		got := luaToGo(luaVal)

		// Special handling for different types
		switch v := original.(type) {
		case float64:
			if v == float64(int64(v)) {
				// Whole numbers come back as int64
				if got != int64(v) {
					t.Errorf("roundtrip(%v) = %v (type %T), want %v", original, got, got, int64(v))
				}
			}
		case []interface{}:
			if arr, ok := got.([]interface{}); !ok {
				t.Errorf("roundtrip(array) = %T, want []interface{}", got)
			} else if len(arr) != len(v) {
				t.Errorf("roundtrip(array) length = %d, want %d", len(arr), len(v))
			}
		default:
			if got != original {
				t.Errorf("roundtrip(%v) = %v (type %T), want %v", original, got, got, original)
			}
		}
	}
}
