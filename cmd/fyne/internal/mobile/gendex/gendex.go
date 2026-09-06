// Copyright 2015 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build gendex

// Gendex generates a dex file used by Go apps created with gomobile.
//
// The dex is a thin extension of NativeActivity, providing access to
// a few platform features (not the SDK UI) not easily accessible from
// NDK headers. Long term these could be made part of the standard NDK,
// however that would limit gomobile to working with newer versions of
// the Android OS, so we do this while we wait.
//
// Requires ANDROID_HOME be set to the path of the Android SDK, and
// javac must be on the PATH.
package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sys/execabs"
)

var outfile = flag.String("o", "dex.go", "result will be written file")

var tmpdir string

func main() {
	flag.Parse()

	var err error
	tmpdir, err = os.MkdirTemp("", "gendex-")
	if err != nil {
		log.Fatal(err)
	}

	err = gendex()
	os.RemoveAll(tmpdir)
	if err != nil {
		log.Fatal(err)
	}
}

func gendex() error {
	androidHome := os.Getenv("ANDROID_HOME")
	if androidHome == "" {
		return errors.New("ANDROID_HOME not set")
	}
	if err := os.MkdirAll(tmpdir+"/work/org/golang/app", 0o775); err != nil {
		return err
	}
	// Try to find Java files - first try relative path (for development), then try GOPATH
	javaFiles, err := filepath.Glob("../../../../internal/driver/mobile/app/*.java")
	if err != nil || len(javaFiles) == 0 {
		// Fallback: look in the current module's vendor directory or GOPATH
		moduleRoot := os.Getenv("GOPATH")
		if moduleRoot == "" {
			moduleRoot = os.Getenv("HOME") + "/go"
		}
		javaFiles, err = filepath.Glob(moduleRoot + "/pkg/mod/fyne.io/fyne/v2@*/internal/driver/mobile/app/*.java")
		if err != nil || len(javaFiles) == 0 {
			// Last resort: try relative to GOPATH/src
			javaFiles, err = filepath.Glob(moduleRoot + "/src/fyne.io/fyne/v2/internal/driver/mobile/app/*.java")
		}
	}
	if err != nil {
		return err
	}
	if len(javaFiles) == 0 {
		return errors.New("could not find internal/driver/mobile/app/*.java files")
	}
	platform, err := findLast(androidHome + "/platforms")
	if err != nil {
		return err
	}
	fmt.Println("gendex: platform", platform)
	cmd := execabs.Command(
		"javac",
		// -parameters gives synthetic/mandated constructor params (e.g. this$0 on
		// anonymous inner classes) a real name. Without it, JDK 21's javac emits a
		// MethodParameters attribute with a null name, which build-tools 34 d8/r8
		// NPEs on ("Cannot invoke String.length() because <parameter1> is null").
		"-parameters",
		"-source", "1.8",
		"-target", "1.8",
		"-bootclasspath", platform+"/android.jar",
		"-d", tmpdir+"/work",
	)
	cmd.Args = append(cmd.Args, javaFiles...)
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Println(cmd.Args)
		os.Stderr.Write(out)
		return err
	}
	buildTools, err := findLast(androidHome + "/build-tools")
	if err != nil {
		return err
	}
	fmt.Println("gendex: build-tools", buildTools)
	// Use d8 instead of dx (dx is deprecated in newer Android SDK versions)
	// Find all .class files in the work directory
	classFiles, err := filepath.Glob(tmpdir + "/work/org/golang/app/*.class")
	if err != nil {
		return err
	}
	cmd = execabs.Command(buildTools + "/d8")
	cmd.Args = append(cmd.Args, "--output", tmpdir, "--classpath", platform+"/android.jar")
	cmd.Args = append(cmd.Args, classFiles...)
	if out, err := cmd.CombinedOutput(); err != nil {
		os.Stderr.Write(out)
		return err
	}
	src, err := os.ReadFile(tmpdir + "/classes.dex")
	if err != nil {
		return err
	}
	data := base64.StdEncoding.EncodeToString(src)

	buf := new(bytes.Buffer)
	fmt.Fprint(buf, header)

	var piece string
	for len(data) > 0 {
		l := 70
		if l > len(data) {
			l = len(data)
		}
		piece, data = data[:l], data[l:]
		fmt.Fprintf(buf, "\t`%s` + \n", piece)
	}
	fmt.Fprintf(buf, "\t``")
	out, err := format.Source(buf.Bytes())
	if err != nil {
		buf.WriteTo(os.Stderr)
		return err
	}

	w, err := os.Create(*outfile)
	if err != nil {
		return err
	}
	if _, err := w.Write(out); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return nil
}

// findLast returns the child of path with the highest version number, e.g.
// platforms/android-35 over android-28 and android-Q (previews sort last),
// build-tools/29.0.2 over 28.0.3. Directory order is not sorted, so relying
// on Readdirnames order picked an arbitrary SDK.
func findLast(path string) (string, error) {
	dir, err := os.Open(path)
	if err != nil {
		return "", err
	}
	children, err := dir.Readdirnames(-1)
	if err != nil {
		return "", err
	}
	if len(children) == 0 {
		return "", errors.New("no entries in " + path)
	}
	sort.SliceStable(children, func(i, j int) bool {
		return versionLess(children[i], children[j])
	})
	return path + "/" + children[len(children)-1], nil
}

// versionKey extracts the numeric components after the last '-' of a name
// ("android-35" -> [35], "29.0.2" -> [29 0 2]). Names without a leading
// number ("android-Q") yield nil and sort before everything numeric.
func versionKey(name string) []int {
	if i := strings.LastIndex(name, "-"); i >= 0 {
		name = name[i+1:]
	}
	var key []int
	for _, part := range strings.Split(name, ".") {
		n := 0
		digits := 0
		for _, r := range part {
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
			digits++
		}
		if digits == 0 {
			break
		}
		key = append(key, n)
	}
	return key
}

func versionLess(a, b string) bool {
	ka, kb := versionKey(a), versionKey(b)
	for i := 0; i < len(ka) && i < len(kb); i++ {
		if ka[i] != kb[i] {
			return ka[i] < kb[i]
		}
	}
	return len(ka) < len(kb)
}

var header = `// Copyright 2015 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Code generated by gendex.go. DO NOT EDIT.

package mobile

var dexStr = `
