package lua

import (
	"fmt"

	lua "github.com/yuin/gopher-lua"
)

// --- Lua/Go type conversion ---

func goToLua(L *lua.LState, v any) lua.LValue {
	if v == nil {
		return lua.LNil
	}
	switch val := v.(type) {
	case bool:
		return lua.LBool(val)
	case float64:
		return lua.LNumber(val)
	case string:
		return lua.LString(val)
	case []interface{}:
		tbl := L.NewTable()
		for i, item := range val {
			tbl.RawSetInt(i+1, goToLua(L, item))
		}
		return tbl
	case map[string]interface{}:
		tbl := L.NewTable()
		for k, item := range val {
			tbl.RawSetString(k, goToLua(L, item))
		}
		return tbl
	default:
		return lua.LString(fmt.Sprintf("%v", val))
	}
}

func luaToGo(v lua.LValue) interface{} {
	switch val := v.(type) {
	case *lua.LNilType:
		return nil
	case lua.LBool:
		return bool(val)
	case lua.LNumber:
		f := float64(val)
		if f == float64(int64(f)) {
			return int64(f)
		}
		return f
	case lua.LString:
		return string(val)
	case *lua.LTable:
		// Detect array: if all keys are 1..n with no gaps, treat as array
		n := val.MaxN()
		if n > 0 {
			arr := make([]interface{}, 0, n)
			isSeq := true
			for i := 1; i <= n; i++ {
				item := val.RawGetInt(i)
				if item == lua.LNil {
					isSeq = false
					break
				}
				arr = append(arr, luaToGo(item))
			}
			if isSeq {
				return arr
			}
		}
		m := make(map[string]interface{})
		val.ForEach(func(k, item lua.LValue) {
			m[lua.LVAsString(k)] = luaToGo(item)
		})
		return m
	default:
		return fmt.Sprintf("%v", val)
	}
}

// --- table helpers ---

func tStr(t *lua.LTable, key string) string {
	v := t.RawGetString(key)
	if v == lua.LNil {
		return ""
	}
	return lua.LVAsString(v)
}

func tStrAlt(t *lua.LTable, keys ...string) string {
	for _, k := range keys {
		if v := tStr(t, k); v != "" {
			return v
		}
	}
	return ""
}
