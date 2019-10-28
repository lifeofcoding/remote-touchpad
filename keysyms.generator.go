// +build ignore

/*
 *    Copyright (c) 2018 Unrud <unrud@outlook.com>
 *
 *    This file is part of Remote-Touchpad.
 *
 *    Remote-Touchpad is free software: you can redistribute it and/or modify
 *    it under the terms of the GNU General Public License as published by
 *    the Free Software Foundation, either version 3 of the License, or
 *    (at your option) any later version.
 *
 *    Remote-Touchpad is distributed in the hope that it will be useful,
 *    but WITHOUT ANY WARRANTY; without even the implied warranty of
 *    MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 *    GNU General Public License for more details.
 *
 *   You should have received a copy of the GNU General Public License
 *   along with Remote-Touchpad.  If not, see <http://www.gnu.org/licenses/>.
 */

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
)

type Keysym int32

const (
	keysymdefHeader  string = "/usr/include/X11/keysymdef.h"
	output           string = "keysyms.generated.go"
	maxMappedUnicode rune   = 0xff
)

var overrideKeysyms = map[string]rune{
	"XK_BackSpace":   0x08,
	"XK_Tab":         0x09,
	"XK_Linefeed":    0x0a,
	"XK_Clear":       0x0b,
	"XK_Return":      0x0d,
	"XK_Pause":       0x13,
	"XK_Scroll_Lock": 0x14,
	"XK_Sys_Req":     0x15,
	"XK_Escape":      0x1b,
}

func main() {
	f, err := os.Open(keysymdefHeader)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	reader := bufio.NewReader(f)
	re := regexp.MustCompile("^\\#define " +
		"(XK_[a-zA-Z_0-9]+)\\s+" + // keysymName
		"0x([0-9a-f]+)\\s*" + // keysym
		"(?:/\\*\\s*(?:U\\+([0-9A-F]{4,6}))?.*\\*/\\s*)?" + // keysymUnicode (optional)
		"$")
	keysymsMap := make(map[rune]Keysym)
	for {
		l, err := reader.ReadString('\n')
		if err == io.EOF {
			break
		} else if err != nil {
			panic(err)
		}
		submatches := re.FindStringSubmatch(l)
		if len(submatches) == 0 {
			continue
		}
		keysymName := submatches[1]
		keysymTemp, err := strconv.ParseInt(submatches[2], 16, 32)
		keysym := Keysym(keysymTemp)
		if err != nil {
			panic(err)
		}
		unicode, found := overrideKeysyms[keysymName]
		if !found {
			if len(submatches[3]) == 0 {
				continue
			}
			unicodeTemp, err := strconv.ParseInt(submatches[3], 16, 32)
			unicode = rune(unicodeTemp)
			if err != nil {
				panic(err)
			}

		}
		if unicode > maxMappedUnicode {
			continue
		}
		if _, found := keysymsMap[unicode]; found {
			continue
		}
		keysymsMap[unicode] = keysym
	}
	content := "package main\n\n" +
		"var keysymsMap = map[rune]Keysym{\n"
	keys := make([]rune, 0)
	for key := range keysymsMap {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	for _, unicode := range keys {
		keysym := keysymsMap[unicode]
		content += fmt.Sprintf("\t0x%04x: 0x%08x,\n", unicode, keysym)
	}
	content += "}\n"
	o, err := os.Create(output)
	if err != nil {
		panic(err)
	}
	defer o.Close()
	if _, err := o.WriteString(content); err != nil {
		panic(err)
	}
}
