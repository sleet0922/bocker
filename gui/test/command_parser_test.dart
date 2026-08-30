import 'package:bocker_gui/main.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

class _FakeBockerCommand extends BockerCommand {
  _FakeBockerCommand({this.containers = '[]', this.mounts = '[]'});

  final String containers;
  final String mounts;
  final calls = <List<String>>[];

  @override
  Future<CommandResult> run(
    List<String> arguments, {
    String? workingDirectory,
    ValueChanged<String>? onOutput,
  }) async {
    calls.add(List<String>.from(arguments));
    if (arguments.length >= 2 &&
        arguments[0] == 'template' &&
        arguments[1] == 'list') {
      return const CommandResult(
        true,
        '[{"distro":"Alpine","release":"3.24","image":"alpine/3.24"}]',
        0,
      );
    }
    if (arguments.length >= 2 &&
        arguments[0] == 'container' &&
        arguments[1] == 'list') {
      return CommandResult(true, containers, 0);
    }
    if (arguments.length >= 5 &&
        arguments[0] == 'container' &&
        arguments[1] == 'set' &&
        arguments[3] == 'mount' &&
        arguments[4] == 'list') {
      return CommandResult(true, mounts, 0);
    }
    return const CommandResult(true, '[]', 0);
  }
}

void main() {
  testWidgets('copies a container value when clicked', (tester) async {
    String? copiedText;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(SystemChannels.platform, (call) async {
          if (call.method == 'Clipboard.setData') {
            copiedText =
                (call.arguments as Map<Object?, Object?>)['text'] as String?;
          }
          return null;
        });
    addTearDown(
      () => TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(SystemChannels.platform, null),
    );

    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(body: CopyableText(text: 'test.cn')),
      ),
    );

    await tester.tap(find.text('test.cn'));
    await tester.pump();

    expect(copiedText, 'test.cn');
    expect(find.text('已复制: test.cn'), findsOneWidget);
  });

  test('parses the container list JSON', () {
    const output = '''[
      {"name":"web","status":"running","network":"nat","ipv4":"10.0.100.24","ipv6":"","memory":"42.0M","domain":"web.test","autostart":"on","ports":"80/tcp(v4,v6), 8080->80/tcp(v4,v6)"},
      {"name":"worker","status":"stopped","network":"bridge","ipv4":"","ipv6":"","memory":"-","domain":"","autostart":"off","ports":"-"}
    ]''';

    final containers = parseContainers(output);

    expect(containers, hasLength(2));
    expect(containers.first.name, 'web');
    expect(containers.first.isRunning, isTrue);
    expect(containers.first.memory, '42.0M');
    expect(containers.first.domain, 'web.test');
    expect(containers.first.portsDisplay, '80/tcp(v4,v6)\n8080->80/tcp(v4,v6)');
    expect(containers.last.network, 'bridge');
  });

  test('rejects malformed container JSON without crashing', () {
    const output = 'not json';

    final containers = parseContainers(output);

    expect(containers, isEmpty);
  });

  test('accepts unexpected JSON field types without crashing', () {
    const output = '[{"name":123,"status":true,"ipv4":null}]';

    final containers = parseContainers(output);

    expect(containers, hasLength(1));
    expect(containers.single.name, '123');
    expect(containers.single.status, 'true');
    expect(containers.single.ipv4, '');
  });

  test('parses file and directory mount summaries', () {
    const output = '''[
      {"name":"mount-readme-1","source":"/home/user/README.md","target":"/work/README.md","readonly":true,"inherited":false},
      {"name":"mount-data-2","source":"/srv/project:blue -> raw","target":"/mnt/data:final -> path","readonly":false,"inherited":true}
    ]''';

    final mounts = parseMounts(output);

    expect(mounts, hasLength(2));
    expect(mounts[0].name, 'mount-readme-1');
    expect(mounts[0].source, '/home/user/README.md');
    expect(mounts[0].target, '/work/README.md');
    expect(mounts[0].readonly, isTrue);
    expect(mounts[1].source, '/srv/project:blue -> raw');
    expect(mounts[1].target, '/mnt/data:final -> path');
    expect(mounts[1].readonly, isFalse);
    expect(mounts[1].inherited, isTrue);
    expect(parseMounts('mount-demo: /host -> /container (rw)'), isEmpty);
  });

  test('normalizes container targets for duplicate detection', () {
    expect(normalizeContainerPath('/mnt/data/'), '/mnt/data');
    expect(normalizeContainerPath('/mnt//data///'), '/mnt/data');
    expect(normalizeContainerPath('/'), '/');
  });

  test('parses local image JSON', () {
    const output = '''[
      {"name":"web-image","size":"12.0M","created":"2026-08-07 10:20","fingerprint":"abc123def456"}
    ]''';

    final images = parseImages(output);

    expect(images, hasLength(1));
    expect(images.single.name, 'web-image');
    expect(images.single.size, '12.0M');
    expect(images.single.fingerprint, 'abc123def456');
  });

  test('parses remote template JSON', () {
    const output = '''[
      {"distro":"Alpine","release":"3.20","image":"alpine/3.20"},
      {"distro":"Debian","release":"12","image":"debian/12"},
      {"distro":"Debian","release":"13","image":"debian/13"}
    ]''';

    final templates = parseImageTemplates(output);

    expect(templates, hasLength(3));
    expect(templates[0].distro, 'Alpine');
    expect(templates[1].image, 'debian/12');
    expect(templates[2].release, '13');
  });

  test('builds canonical template install arguments', () {
    final args = templateInstallArguments(
      image: 'debian/12',
      name: 'demo',
      network: 'nat',
    );

    expect(args, [
      'template',
      'install',
      'debian/12',
      '--network',
      'nat',
      '--name',
      'demo',
    ]);
  });

  test('splits exec commands into argv like a shell', () {
    expect(splitShellWords('uname -a'), ['uname', '-a']);
    expect(splitShellWords("echo 'hello world'"), ['echo', 'hello world']);
    expect(splitShellWords('cat "a b"'), ['cat', 'a b']);
    expect(splitShellWords('sh -c \'echo hi | cat\''), [
      'sh',
      '-c',
      'echo hi | cat',
    ]);
    expect(splitShellWords('  spaced   out  '), ['spaced', 'out']);
    expect(splitShellWords(''), isEmpty);
    expect(splitShellWords('   '), isEmpty);
    expect(splitShellWords('a\\ b'), ['a b']);
    // Unbalanced quotes degrade gracefully instead of crashing.
    expect(splitShellWords('echo "oops'), ['echo', 'oops']);
  });

  test('builds canonical image run arguments', () {
    final defaults = imageRunArguments(
      image: 'web-image',
      name: 'web-01',
      network: 'default',
    );
    final overridden = imageRunArguments(
      image: 'web-image',
      name: 'web-02',
      network: 'nat',
    );

    expect(defaults, ['image', 'run', 'web-image', '--name', 'web-01']);
    expect(overridden, [
      'image',
      'run',
      'web-image',
      '--name',
      'web-02',
      '--network',
      'nat',
    ]);
  });

  test('validates names, domains, and port mappings', () {
    expect(isValidBockerName('web-01'), isTrue);
    expect(isValidBockerName('Web-01'), isTrue);
    expect(isValidBockerName('123'), isFalse);
    expect(isValidBockerName('-web'), isFalse);
    expect(isValidBockerName('bad_name'), isFalse);

    expect(isValidDomain('app.test'), isTrue);
    expect(isValidDomain('-app.test'), isFalse);
    expect(isValidDomain('app..test'), isFalse);

    for (final value in ['80', '8080:80', '53/udp', '8080:80/tcp']) {
      expect(isValidPortMapping(value), isTrue, reason: value);
    }
    for (final value in ['0', '65536', 'a:80', '80/sctp']) {
      expect(isValidPortMapping(value), isFalse, reason: value);
    }
  });

  test('extracts removable host ports from list summaries', () {
    expect(
      removablePortSpecs('80/tcp(v4,v6), 8080->80/tcp(v4,v6), 53/udp(v4)'),
      ['80/tcp', '8080/tcp', '53/udp'],
    );
  });

  testWidgets('uses cards on compact windows without overflow', (tester) async {
    tester.view.physicalSize = const Size(760, 620);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    const containers = '''[
      {"name":"web","status":"running","network":"nat","ipv4":"10.0.100.24","ipv6":"fd42::24","memory":"42.0M","domain":"web.test","autostart":"on","ports":"8080->80/tcp(v4,v6)"}
    ]''';

    await tester.pumpWidget(
      MaterialApp(
        home: BockerHome(command: _FakeBockerCommand(containers: containers)),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byType(DataTable), findsNothing);
    expect(find.text('web'), findsOneWidget);
    expect(find.text('10.0.100.24'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('uses the dense table on wide windows', (tester) async {
    tester.view.physicalSize = const Size(1600, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    const containers = '''[
      {"name":"worker","status":"stopped","network":"bridge","ipv4":"","ipv6":"","domain":"","autostart":"off","ports":"-"}
    ]''';

    await tester.pumpWidget(
      MaterialApp(
        home: BockerHome(command: _FakeBockerCommand(containers: containers)),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byType(DataTable), findsOneWidget);
    expect(find.text('worker'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('new installs default to NAT networking', (tester) async {
    tester.view.physicalSize = const Size(1100, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(
      MaterialApp(home: BockerHome(command: _FakeBockerCommand())),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('安装容器'));
    await tester.pumpAndSettle();

    final selectors = tester.widgetList<SegmentedButton<String>>(
      find.byType(SegmentedButton<String>),
    );
    expect(selectors, isNotEmpty);
    expect(selectors.first.selected, {'nat'});
    expect(tester.takeException(), isNull);
  });

  testWidgets('settings lists and removes existing mounts', (tester) async {
    tester.view.physicalSize = const Size(1100, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    const containers = '''[
      {"name":"worker","status":"stopped","network":"nat","ipv4":"","ipv6":"","domain":"","autostart":"off","ports":"-"}
    ]''';
    final command = _FakeBockerCommand(
      containers: containers,
      mounts:
          '[{"name":"mount-data-1","source":"/tmp/data","target":"/mnt/data","readonly":true,"inherited":false}]',
    );

    await tester.pumpWidget(MaterialApp(home: BockerHome(command: command)));
    await tester.pumpAndSettle();
    await tester.tap(find.byTooltip('更多操作').first);
    await tester.pumpAndSettle();
    await tester.tap(find.text('设置'));
    await tester.pumpAndSettle();

    expect(find.text('/tmp/data'), findsOneWidget);
    expect(find.text('→ /mnt/data'), findsOneWidget);
    expect(find.text('ro'), findsOneWidget);
    expect(
      command.calls.any(
        (call) =>
            call.join('\u0000') ==
            const [
              'container',
              'set',
              'worker',
              'mount',
              'list',
              '--json',
            ].join('\u0000'),
      ),
      isTrue,
    );

    await tester.tap(find.byTooltip('删除挂载'));
    await tester.pump();
    expect(find.text('1 个挂载将在保存时删除'), findsOneWidget);
    await tester.tap(find.text('保存'));
    await tester.pumpAndSettle();

    expect(
      command.calls.any(
        (call) =>
            call.join('\u0000') ==
            const [
              'container',
              'set',
              'worker',
              'mount',
              'rm',
              'mount-data-1',
            ].join('\u0000'),
      ),
      isTrue,
    );
    expect(tester.takeException(), isNull);
  });

  testWidgets('running containers disable mount editing', (tester) async {
    tester.view.physicalSize = const Size(1100, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    const containers = '''[
      {"name":"web","status":"running","network":"nat","ipv4":"10.0.100.24","ipv6":"","domain":"","autostart":"off","ports":"-"}
    ]''';
    final command = _FakeBockerCommand(
      containers: containers,
      mounts:
          '[{"name":"mount-data-1","source":"/tmp/data","target":"/mnt/data","readonly":false,"inherited":false}]',
    );

    await tester.pumpWidget(MaterialApp(home: BockerHome(command: command)));
    await tester.pumpAndSettle();
    await tester.tap(find.byTooltip('更多操作').first);
    await tester.pumpAndSettle();
    await tester.tap(find.text('设置'));
    await tester.pumpAndSettle();

    expect(find.text('停止容器后才能添加、删除或修改挂载'), findsOneWidget);
    final sourceField = tester.widget<TextField>(
      find.byWidgetPredicate(
        (widget) =>
            widget is TextField && widget.decoration?.labelText == '宿主机源路径',
      ),
    );
    expect(sourceField.enabled, isFalse);
    expect(find.byTooltip('停止容器后才能删除挂载'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('settings updates an existing mount mode atomically', (tester) async {
    tester.view.physicalSize = const Size(1100, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    const containers = '''[
      {"name":"worker","status":"stopped","network":"nat","ipv4":"","ipv6":"","domain":"","autostart":"off","ports":"-"}
    ]''';
    final command = _FakeBockerCommand(
      containers: containers,
      mounts:
          '[{"name":"mount-data-1","source":"/tmp/data","target":"/mnt/data","readonly":true,"inherited":false}]',
    );

    await tester.pumpWidget(MaterialApp(home: BockerHome(command: command)));
    await tester.pumpAndSettle();
    await tester.tap(find.byTooltip('更多操作').first);
    await tester.pumpAndSettle();
    await tester.tap(find.text('设置'));
    await tester.pumpAndSettle();

    await tester.tap(find.text('rw'));
    await tester.pump();
    await tester.tap(find.text('保存'));
    await tester.pumpAndSettle();

    expect(
      command.calls.any(
        (call) =>
            call.join('\u0000') ==
            const [
              'container',
              'set',
              'worker',
              'mount',
              'update',
              'mount-data-1',
              'rw',
            ].join('\u0000'),
      ),
      isTrue,
    );
    expect(
      command.calls.any(
        (call) =>
            call.length >= 5 &&
            call[0] == 'container' &&
            call[1] == 'set' &&
            call[3] == 'mount' &&
            call[4] == 'rm',
      ),
      isFalse,
    );
    expect(tester.takeException(), isNull);
  });

  testWidgets('queues a read-only file mount and submits its mode', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(1100, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    const containers = '''[
      {"name":"worker","status":"stopped","network":"nat","ipv4":"","ipv6":"","domain":"","autostart":"off","ports":"-"}
    ]''';
    final command = _FakeBockerCommand(containers: containers);

    await tester.pumpWidget(MaterialApp(home: BockerHome(command: command)));
    await tester.pumpAndSettle();
    await tester.tap(find.byTooltip('更多操作').first);
    await tester.pumpAndSettle();
    await tester.tap(find.text('设置'));
    await tester.pumpAndSettle();

    final source = find.byWidgetPredicate(
      (widget) =>
          widget is TextField && widget.decoration?.labelText == '宿主机源路径',
    );
    final target = find.byWidgetPredicate(
      (widget) =>
          widget is TextField && widget.decoration?.labelText == '容器内目标路径',
    );
    await tester.enterText(source, '/tmp/readme.txt');
    await tester.enterText(target, '/etc/readme.txt');
    await tester.tap(find.text('只读 (ro)'));
    await tester.tap(find.text('加入待添加列表'));
    await tester.pump();

    expect(find.text('→ /etc/readme.txt · ro · 待添加'), findsOneWidget);
    await tester.tap(find.text('保存'));
    await tester.pumpAndSettle();

    expect(
      command.calls.any(
        (call) =>
            call.join('\u0000') ==
            const [
              'container',
              'set',
              'worker',
              'mount',
              'add',
              '/tmp/readme.txt',
              '/etc/readme.txt',
              'ro',
            ].join('\u0000'),
      ),
      isTrue,
    );
    expect(tester.takeException(), isNull);
  });
}
