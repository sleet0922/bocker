import 'package:bocker_gui/main.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('parses the container list JSON', () {
    const output = '''[
      {"name":"web","status":"running","network":"nat","ipv4":"10.0.100.24","ipv6":"","autostart":"on","ports":"8080/tcp -> 80"},
      {"name":"worker","status":"stopped","network":"bridge","ipv4":"","ipv6":"","autostart":"off","ports":"-"}
    ]''';

    final containers = parseContainers(output);

    expect(containers, hasLength(2));
    expect(containers.first.name, 'web');
    expect(containers.first.isRunning, isTrue);
    expect(containers.first.ports, '8080/tcp -> 80');
    expect(containers.last.network, 'bridge');
  });

  test('rejects malformed container JSON without crashing', () {
    const output = 'not json';

    final containers = parseContainers(output);

    expect(containers, isEmpty);
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
      permission: 'normal',
    );

    expect(args, [
      'template',
      'install',
      'debian/12',
      '--network',
      'nat',
      '--permission',
      'normal',
      '--name',
      'demo',
    ]);
  });

  test('builds canonical image run arguments', () {
    final defaults = imageRunArguments(
      image: 'web-image',
      name: 'web-01',
      network: 'default',
      permission: 'normal',
    );
    final overridden = imageRunArguments(
      image: 'web-image',
      name: 'web-02',
      network: 'nat',
      permission: 'super',
    );

    expect(defaults, [
      'image',
      'run',
      'web-image',
      '--name',
      'web-01',
      '--permission',
      'normal',
    ]);
    expect(overridden, [
      'image',
      'run',
      'web-image',
      '--name',
      'web-02',
      '--permission',
      'super',
      '--network',
      'nat',
    ]);
  });
}
