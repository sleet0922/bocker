import 'package:bocker_gui/main.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('parses the bocker list table', () {
    const output = '''
NAME       STATUS    NETWORK  IPV4          IPV6  AUTOSTART  PORTS
web        running   nat      10.0.100.24   -     on         8080/tcp -> 80
worker     stopped   bridge   -             -     off        -
''';

    final containers = parseContainers(output);

    expect(containers, hasLength(2));
    expect(containers.first.name, 'web');
    expect(containers.first.isRunning, isTrue);
    expect(containers.first.ports, '8080/tcp -> 80');
    expect(containers.last.network, 'bridge');
  });

  test('parses a list whose terminal collapsed the column padding', () {
    const output = 'api running nat 10.0.100.20 - on 8080/tcp -> 80\n';

    final containers = parseContainers(output);

    expect(containers, hasLength(1));
    expect(containers.single.name, 'api');
    expect(containers.single.ports, '8080/tcp -> 80');
  });

  test('parses local image rows and ignores the table frame', () {
    const output = '''
╭─ 本地镜像 (共 1 个)
│ 别名                          大小      创建时间          指纹(短)
│ ──────────────────────────── ──────── ──────────────── ────────────
│ web-image                     12.0M    2026-08-07 10:20  abc123def456
╰─
''';

    final images = parseImages(output);

    expect(images, hasLength(1));
    expect(images.single.name, 'web-image');
    expect(images.single.size, '12.0M');
    expect(images.single.fingerprint, 'abc123def456');
  });

  test('parses remote templates from build show', () {
    const output = '''
╭─ 可用基础镜像 (架构: x86_64, 共 2 个发行版 3 个版本)
│ 可直接用于 Incusfile 的 FROM 指令
│
│ Alpine
│   FROM alpine/3.20   # 3.20
│ Debian
│   FROM debian/12   # 12
│   FROM debian/13   # 13
│
╰─
''';

    final templates = parseImageTemplates(output);

    expect(templates, hasLength(3));
    expect(templates[0].distro, 'Alpine');
    expect(templates[1].image, 'debian/12');
    expect(templates[2].release, '13');
  });
}
