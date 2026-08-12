package bocker

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// writeIncusfile 写入临时 Incusfile 并返回其路径。
func writeIncusfile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("写入测试 Incusfile 失败: %v", err)
	}
	return p
}

func TestTempBlockBasic(t *testing.T) {
	content := `FROM debian/13
NAME my-app
RUN apt-get install -y ca-certificates

TEMP builder
  RUN apt-get install -y golang-go
  WORKDIR /src
  COPY ./main.go .
  RUN go build -o app .
END

COPY --from=builder /src/app /usr/local/bin/app
EXPOSE 8080/tcp
AUTOSTART on
`
	p := writeIncusfile(t, "Incusfile", content)
	f, err := parseIncusfile(p)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}

	// 期望 2 个阶段: [builder, main]
	if len(f.Stages) != 2 {
		t.Fatalf("期望 2 个阶段 (builder + main), 实际 %d", len(f.Stages))
	}
	if f.Stages[0].Name != "builder" {
		t.Errorf("阶段0 名字期望 builder, 实际 %q", f.Stages[0].Name)
	}
	if f.Stages[0].From != "debian/13" {
		t.Errorf("阶段0 From 期望继承 debian/13, 实际 %q", f.Stages[0].From)
	}
	if len(f.Stages[0].Steps) != 4 {
		t.Errorf("阶段0 步骤数期望 4 (RUN/WORKDIR/COPY/RUN), 实际 %d", len(f.Stages[0].Steps))
	}
	// 主阶段 (最终阶段) 无 AS 名字
	if f.Stages[1].Name != "" {
		t.Errorf("主阶段名字期望空, 实际 %q", f.Stages[1].Name)
	}
	// 主阶段步骤: RUN apt + COPY --from=builder
	if len(f.Stages[1].Steps) != 2 {
		t.Errorf("主阶段步骤数期望 2, 实际 %d", len(f.Stages[1].Steps))
	}
	// 第二个步骤是 COPY --from=builder
	copyStep := f.Stages[1].Steps[1]
	if copyStep.Kind != "COPY" || copyStep.Copy.From != "builder" {
		t.Errorf("主阶段第二个步骤期望 COPY --from=builder, 实际 %+v", copyStep)
	}
	// 顶层字段同步自最终阶段
	if f.Name != "my-app" {
		t.Errorf("f.Name 期望 my-app, 实际 %q", f.Name)
	}
	if len(f.Exposes) != 1 || f.Exposes[0].Port != 8080 {
		t.Errorf("f.Exposes 期望 [8080], 实际 %+v", f.Exposes)
	}
	if f.Autostart == nil || !*f.Autostart {
		t.Errorf("f.Autostart 期望 on, 实际 %v", f.Autostart)
	}
}

func TestTempBlockUnclosed(t *testing.T) {
	content := `FROM debian/13
TEMP builder
  RUN echo hi
`
	p := writeIncusfile(t, "Incusfile", content)
	_, err := parseIncusfile(p)
	if err == nil {
		t.Fatal("未关闭的 TEMP 块应报错")
	}
}

func TestTempBlockNested(t *testing.T) {
	content := `FROM debian/13
TEMP a
  TEMP b
  END
END
`
	p := writeIncusfile(t, "Incusfile", content)
	_, err := parseIncusfile(p)
	if err == nil {
		t.Fatal("嵌套 TEMP 应报错")
	}
}

func TestTempBlockMultipleFromRejected(t *testing.T) {
	content := `FROM debian/13 AS x
RUN echo a
FROM debian/13
TEMP t
  RUN echo b
END
`
	p := writeIncusfile(t, "Incusfile", content)
	_, err := parseIncusfile(p)
	if err == nil {
		t.Fatal("TEMP 块与多 FROM 混用应报错")
	}
}

func TestTempBlockMultipleTemps(t *testing.T) {
	content := `FROM debian/13
TEMP a
  RUN echo a
END
TEMP b
  RUN echo b
END
RUN echo final
`
	p := writeIncusfile(t, "Incusfile", content)
	f, err := parseIncusfile(p)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	// 期望 3 个阶段: [a, b, main]
	if len(f.Stages) != 3 {
		t.Fatalf("期望 3 个阶段, 实际 %d", len(f.Stages))
	}
	wantNames := []string{"a", "b", ""}
	gotNames := []string{f.Stages[0].Name, f.Stages[1].Name, f.Stages[2].Name}
	if !reflect.DeepEqual(wantNames, gotNames) {
		t.Errorf("阶段名顺序期望 %v, 实际 %v", wantNames, gotNames)
	}
}

func TestEndWithoutTemp(t *testing.T) {
	content := `FROM debian/13
RUN echo hi
END
`
	p := writeIncusfile(t, "Incusfile", content)
	_, err := parseIncusfile(p)
	if err == nil {
		t.Fatal("无 TEMP 的 END 应报错")
	}
}

func TestNetworkDirective(t *testing.T) {
	p := writeIncusfile(t, "Incusfile", "FROM debian:12\nNETWORK nat\n")
	f, err := parseIncusfile(p)
	if err != nil {
		t.Fatalf("解析 NETWORK 失败: %v", err)
	}
	if f.Network != "nat" {
		t.Fatalf("NETWORK 应解析为 nat，得到 %q", f.Network)
	}
}

func TestNetworkDirectiveRejectsIncusNames(t *testing.T) {
	p := writeIncusfile(t, "Incusfile", "FROM debian:12\nNETWORK macvlan\n")
	if _, err := parseIncusfile(p); err == nil {
		t.Fatal("NETWORK macvlan 应被拒绝，用户接口只支持 bridge/nat")
	}
}

func TestCommandDirectives(t *testing.T) {
	content := `FROM debian/12
ENTRYPOINT ["/usr/local/bin/app", "--listen", "[::]:8080"]
CMD ["--workers", "2"]
`
	p := writeIncusfile(t, "Incusfile", content)
	f, err := parseIncusfile(p)
	if err != nil {
		t.Fatalf("parse CMD/ENTRYPOINT: %v", err)
	}
	if got, want := f.Entrypoint, []string{"/usr/local/bin/app", "--listen", "[::]:8080"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ENTRYPOINT = %#v, want %#v", got, want)
	}
	if got, want := f.Cmd, []string{"--workers", "2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("CMD = %#v, want %#v", got, want)
	}
	if got, want := runtimeCommand(f), []string{"/usr/local/bin/app", "--listen", "[::]:8080", "--workers", "2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("runtimeCommand = %#v, want %#v", got, want)
	}
}

func TestCommandDirectiveRequiresJSONExecForm(t *testing.T) {
	p := writeIncusfile(t, "Incusfile", "FROM alpine/3.24\nCMD /usr/bin/app --message 'hello world'\n")
	if _, err := parseIncusfile(p); err == nil {
		t.Fatal("shell-form CMD should fail rather than silently change execution semantics")
	}
	p = writeIncusfile(t, "Incusfile", "FROM alpine/3.24\nENTRYPOINT []\n")
	if _, err := parseIncusfile(p); err == nil {
		t.Fatal("empty ENTRYPOINT should fail")
	}
}

func TestRuntimeDirectivesMustBeInFinalStage(t *testing.T) {
	p := writeIncusfile(t, "Incusfile", "FROM alpine/3.24 AS builder\nEXPOSE 8080\nFROM alpine/3.24\n")
	if _, err := parseIncusfile(p); err == nil {
		t.Fatal("runtime directive in a non-final stage should fail")
	}
}

func TestNormalizeImageRefPreservesRemoteAndTag(t *testing.T) {
	for input, want := range map[string]string{
		"debian:12":             "debian/12",
		"images:debian:12":      "images:debian/12",
		"images:ubuntu/24.04":   "images:ubuntu/24.04",
		"registry:5000/app:1.2": "registry:5000/app:1.2",
	} {
		if got := normalizeImageRef(input); got != want {
			t.Errorf("normalizeImageRef(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestPinnedFromImageAndValidation(t *testing.T) {
	fingerprint := strings.Repeat("a", 64)
	p := writeIncusfile(t, "Incusfile", "FROM images:debian:12@"+fingerprint+"\n")
	f, err := parseIncusfile(p)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := f.Stages[0].From, "images:debian/12"; got != want {
		t.Fatalf("From = %q, want %q", got, want)
	}
	if got := f.Stages[0].BaseFingerprint; got != fingerprint {
		t.Fatalf("BaseFingerprint = %q", got)
	}
	p = writeIncusfile(t, "Incusfile", "FROM alpine/3.24@bad\n")
	if _, err := parseIncusfile(p); err == nil {
		t.Fatal("short fingerprint should fail")
	}
}

func TestDuplicateExposeRejected(t *testing.T) {
	p := writeIncusfile(t, "Incusfile", "FROM alpine/3.24\nEXPOSE 8080 8080/tcp\n")
	if _, err := parseIncusfile(p); err == nil {
		t.Fatal("duplicate EXPOSE should fail")
	}
}

func TestCopyContextPathAndFallback(t *testing.T) {
	for _, source := range []string{"../outside", "/etc/passwd", ".", "sub/../../outside"} {
		if _, err := copyContextRelativePath("/tmp/context", source); err == nil {
			t.Errorf("copyContextRelativePath(%q) should fail", source)
		}
	}
	contextDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(contextDir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(contextDir, "nested", "file"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	contextFD, err := unix.Open(contextDir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(contextFD)
	fd, err := openContextEntryFallback(contextFD, "nested/file", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW)
	if err != nil {
		t.Fatal(err)
	}
	unix.Close(fd)
	if err := os.Symlink("file", filepath.Join(contextDir, "nested", "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := openContextEntryFallback(contextFD, "nested/link", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW); err == nil {
		t.Fatal("fallback should reject symlink")
	}
}

func TestFromPayloadAcceptsWhitespaceAroundAS(t *testing.T) {
	p := writeIncusfile(t, "Incusfile", "FROM debian:12\tAS\tbase\nRUN echo ok\n")
	f, err := parseIncusfile(p)
	if err != nil {
		t.Fatalf("解析 FROM AS 失败: %v", err)
	}
	if len(f.Stages) != 1 || f.Stages[0].From != "debian/12" || f.Stages[0].Name != "base" {
		t.Fatalf("FROM AS 解析结果错误: %+v", f.Stages)
	}
}

func TestNetworkDirectiveCannotBeInsideTemp(t *testing.T) {
	p := writeIncusfile(t, "Incusfile", "FROM debian:12\nTEMP build\nNETWORK nat\nEND\n")
	if _, err := parseIncusfile(p); err == nil {
		t.Fatal("TEMP 内 NETWORK 应被拒绝")
	}
}

func TestFromRejectsMalformedAndDuplicateStageNames(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "missing stage after AS", content: "FROM debian:12 AS\n"},
		{name: "too many fields", content: "FROM debian:12 AS build extra\n"},
		{name: "numeric stage", content: "FROM debian:12 AS 0\n"},
		{name: "invalid stage", content: "FROM debian:12 AS build/stage\n"},
		{name: "duplicate case insensitive", content: "FROM debian:12 AS Builder\nFROM debian:12 AS builder\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := writeIncusfile(t, "Incusfile", tc.content)
			if _, err := parseIncusfile(p); err == nil {
				t.Fatalf("应拒绝:\n%s", tc.content)
			}
		})
	}
}

func TestCopyFromRejectsEmptyOrInvalidReference(t *testing.T) {
	for _, content := range []string{
		"FROM debian:12\nCOPY --from= /src /dst\n",
		"FROM debian:12\nCOPY --from=-1 /src /dst\n",
		"FROM debian:12\nCOPY --from=bad/name /src /dst\n",
	} {
		p := writeIncusfile(t, "Incusfile", content)
		if _, err := parseIncusfile(p); err == nil {
			t.Fatalf("应拒绝:\n%s", content)
		}
	}
}

func TestShellSplitQuotesEscapesAndEmptyArguments(t *testing.T) {
	got, err := shellSplit(`alpha "two words" 'three words' four\ five "" '' "a\"b" "c\qd"`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"alpha", "two words", "three words", "four five", "", "", `a"b`, `c\qd`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("shellSplit = %#v, want %#v", got, want)
	}
	for _, input := range []string{`"unterminated`, `trailing\`} {
		if _, err := shellSplit(input); err == nil {
			t.Fatalf("shellSplit(%q) 应失败", input)
		}
	}
}

func TestEnvQuotingAndValidation(t *testing.T) {
	p := writeIncusfile(t, "Incusfile", `FROM debian:12
ENV GREETING="hello world"
ENV EMPTY=""
ENV ESCAPED=hello\ world
`)
	f, err := parseIncusfile(p)
	if err != nil {
		t.Fatal(err)
	}
	want := []EnvSpec{
		{Key: "GREETING", Value: "hello world"},
		{Key: "EMPTY", Value: ""},
		{Key: "ESCAPED", Value: "hello world"},
	}
	var got []EnvSpec
	for _, step := range f.Steps {
		got = append(got, step.Env)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ENV = %#v, want %#v", got, want)
	}

	for _, key := range []string{"1BAD", "BAD-NAME", "BAD.NAME"} {
		p := writeIncusfile(t, "Incusfile", "FROM debian:12\nENV "+key+"=value\n")
		if _, err := parseIncusfile(p); err == nil {
			t.Fatalf("ENV 名称 %q 应被拒绝", key)
		}
	}
}

func TestEOFContinuationIsRejected(t *testing.T) {
	p := writeIncusfile(t, "Incusfile", "FROM debian:12\nRUN echo \\")
	if _, err := parseIncusfile(p); err == nil || !strings.Contains(err.Error(), "续行") {
		t.Fatalf("文件尾续行应报错，得到 %v", err)
	}
}

func TestEvenTrailingBackslashesAreNotContinuation(t *testing.T) {
	p := writeIncusfile(t, "Incusfile", `FROM debian:12
RUN printf \\`)
	f, err := parseIncusfile(p)
	if err != nil {
		t.Fatalf("偶数个反斜杠不应触发续行: %v", err)
	}
	if len(f.Steps) != 1 || f.Steps[0].Run != `printf \\` {
		t.Fatalf("RUN 内容错误: %#v", f.Steps)
	}
}

func TestContinuationPreservesShellSpacing(t *testing.T) {
	p := writeIncusfile(t, "Incusfile", "FROM alpine/3.24\nRUN printf foo\\\n  bar\nRUN echo left && \\\n  echo right\n")
	f, err := parseIncusfile(p)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := f.Steps[0].Run, "printf foobar"; got != want {
		t.Fatalf("first continued RUN = %q, want %q", got, want)
	}
	if got, want := f.Steps[1].Run, "echo left && echo right"; got != want {
		t.Fatalf("second continued RUN = %q, want %q", got, want)
	}
}

func TestRelativeWorkdirAndCopyDirectoryTarget(t *testing.T) {
	p := writeIncusfile(t, "Incusfile", "FROM debian:12\nWORKDIR /srv\nWORKDIR app\nWORKDIR ../shared\nCOPY ./artifact bin/\n")
	f, err := parseIncusfile(p)
	if err != nil {
		t.Fatal(err)
	}
	workdir := "/"
	for _, step := range f.Steps {
		if step.Kind == "WORKDIR" {
			if filepath.IsAbs(step.Workdir) {
				workdir = filepath.ToSlash(filepath.Clean(step.Workdir))
			} else {
				workdir = resolveContainerPath(workdir, step.Workdir)
			}
		}
	}
	if workdir != "/srv/shared" {
		t.Fatalf("最终 WORKDIR = %q", workdir)
	}
	dst := resolveContainerPath(workdir, f.Steps[len(f.Steps)-1].Copy.Dst)
	if dst != "/srv/shared/bin/" {
		t.Fatalf("COPY 目录目标 = %q", dst)
	}
	_, _, target, err := crossContainerCopyPaths("/out/artifact", dst, true)
	if err != nil || target != "/srv/shared/bin/artifact" {
		t.Fatalf("跨阶段 COPY 目标 = %q, %v", target, err)
	}
}

func TestResolvePriorStageIsCaseInsensitiveAndRejectsForwardReferences(t *testing.T) {
	stages := []Stage{{Name: "Builder"}, {Name: "assets"}, {}}
	index, err := resolvePriorStage(stages, 2, "builder")
	if err != nil || index != 0 {
		t.Fatalf("大小写不敏感引用失败: index=%d err=%v", index, err)
	}
	if _, err := resolvePriorStage(stages, 1, "assets"); err == nil {
		t.Fatal("应拒绝前向阶段引用")
	}
	if _, err := resolvePriorStage(stages, 1, "1"); err == nil {
		t.Fatal("应拒绝当前阶段的数字引用")
	}
}

func TestCopyRejectsPathTraversalAndSymlinkAtKernelBoundary(t *testing.T) {
	contextDir := t.TempDir()
	outside := filepath.Join(filepath.Dir(contextDir), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := applyCopyDst(nil, "unused", CopySpec{Src: "../outside.txt"}, contextDir, "/tmp/outside"); err == nil {
		t.Fatal("COPY 路径穿越应被拒绝")
	}

	realDir := t.TempDir()
	link := filepath.Join(contextDir, "linked")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("当前系统不能创建测试符号链接: %v", err)
	}
	contextFD, err := unix.Open(contextDir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(contextFD)
	if _, err := openContextEntry(contextFD, "linked/file", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW); err == nil {
		t.Fatal("内核路径解析应拒绝中间符号链接")
	}
}

func TestNameAndDomainValidationInIncusfile(t *testing.T) {
	for _, line := range []string{
		"NAME bad name",
		"NAME -bad",
		"NAME bad_name",
		"DOMAIN bad domain",
		"DOMAIN -bad.example",
		"DOMAIN bad_example",
	} {
		p := writeIncusfile(t, "Incusfile", "FROM debian:12\n"+line+"\n")
		if _, err := parseIncusfile(p); err == nil {
			t.Fatalf("%q 应被拒绝", line)
		}
	}
}

func TestEnvPersistenceHelpers(t *testing.T) {
	got := dedupeEnvSpecs([]EnvSpec{
		{Key: "A", Value: "old"},
		{Key: "B", Value: "two"},
		{Key: "A", Value: "new"},
	})
	want := []EnvSpec{{Key: "B", Value: "two"}, {Key: "A", Value: "new"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("去重结果 = %#v, want %#v", got, want)
	}
	if got := shellQuote(`a b'c`); got != `'a b'"'"'c'` {
		t.Fatalf("shellQuote = %q", got)
	}
	if got := quoteEnvironmentValue("a\\b\"c"); got != `"a\\b\"c"` {
		t.Fatalf("quoteEnvironmentValue = %q", got)
	}
}

func TestRuntimeEnvironmentMetadata(t *testing.T) {
	f := &Incusfile{Env: []EnvSpec{{Key: "A", Value: "first"}, {Key: "A", Value: "final"}, {Key: "B", Value: "two"}}}
	properties := buildImageProperties(f)
	parsed, err := runtimeConfigFromImageProperties("test", properties)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := incusEnvironmentConfig(parsed.Env), map[string]string{"environment.A": "final", "environment.B": "two"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("environment config = %#v, want %#v", got, want)
	}
}

func TestDefaultNameFromImageProducesValidName(t *testing.T) {
	for _, image := range []string{
		"debian:12",
		"images:ubuntu/24.04/cloud",
		"###",
		"123",
		strings.Repeat("very-long-image/", 10),
	} {
		name := defaultNameFromImage(image)
		if err := validateBockerName(name); err != nil {
			t.Fatalf("defaultNameFromImage(%q) = %q: %v", image, name, err)
		}
	}
}

func TestDefaultExecEnvLeavesPATHToIncus(t *testing.T) {
	env := defaultExecEnv()
	if _, ok := env["PATH"]; ok {
		t.Fatal("default exec environment must not override the container PATH")
	}
	for key := range map[string]string{"HOME": "/root", "TERM": "", "USER": "root"} {
		if env[key] == "" {
			t.Fatalf("default exec environment is missing %s", key)
		}
	}
}
