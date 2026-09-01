import 'package:flutter_test/flutter_test.dart';
import 'package:bocker_mobile/models.dart';

void main() {
  test('parseContainers 解析容器 JSON', () {
    const json = '''
    [{"name":"web-01","status":"RUNNING","network":"nat","ipv4":"10.0.0.5","ipv6":"","memory":"512MB","domain":"web.example.com","autostart":"yes","ports":"8080:80, 443:443"},
     {"name":"db","status":"STOPPED","network":"bridge","ipv4":"","ipv6":"","memory":"","domain":"","autostart":"no","ports":""}]
    ''';
    final containers = parseContainers(json);
    expect(containers.length, 2);
    expect(containers[0].name, 'web-01');
    expect(containers[0].isRunning, true);
    expect(containers[0].portList, ['8080:80', '443:443']);
    expect(containers[1].isRunning, false);
    expect(containers[1].portList, isEmpty);
  });

  test('parseContainers 容错非法输出', () {
    expect(parseContainers('not json'), isEmpty);
    expect(parseContainers('{"a":1}'), isEmpty);
  });

  test('parseImages 解析镜像 JSON', () {
    const json =
        '[{"name":"web-image","size":"123.4M","created":"2026-01-01 10:00","fingerprint":"abcdef123456"}]';
    final images = parseImages(json);
    expect(images.single.name, 'web-image');
    expect(images.single.size, '123.4M');
    expect(images.single.fingerprint, 'abcdef123456');
  });

  test('parseImageTemplates 解析模板 JSON', () {
    const json =
        '[{"distro":"debian","release":"12","image":"debian/12"},{"distro":"ubuntu","release":"24.04","image":"ubuntu/24.04"}]';
    final templates = parseImageTemplates(json);
    expect(templates.length, 2);
    expect(templates[0].distro, 'debian');
    expect(templates[1].release, '24.04');
  });

  test('parseMounts 解析挂载 JSON', () {
    const json =
        '[{"name":"mount-abc","source":"/data/www","target":"/var/www","readonly":true,"inherited":false},'
        '{"name":"mount-def","source":"/opt/conf","target":"/etc/conf","readonly":false,"inherited":true}]';
    final mounts = parseMounts(json);
    expect(mounts.length, 2);
    expect(mounts[0].name, 'mount-abc');
    expect(mounts[0].readonly, isTrue);
    expect(mounts[0].modeLabel, 'ro');
    expect(mounts[0].displayLabel, '/data/www -> /var/www (ro)');
    expect(mounts[1].readonly, isFalse);
    expect(mounts[1].inherited, isTrue);
    expect(mounts[1].displayLabel.contains('[镜像继承]'), isTrue);
  });

  test('parseMounts 容错非法输出', () {
    expect(parseMounts('not json'), isEmpty);
    expect(parseMounts('[]'), isEmpty);
  });

  test('isValidBockerName 命名规则', () {
    expect(isValidBockerName('web-01'), isTrue);
    expect(isValidBockerName('a'), isTrue);
    expect(isValidBockerName('A1-b'), isTrue);
    expect(isValidBockerName('-bad'), isFalse);
    expect(isValidBockerName('bad-'), isFalse);
    expect(isValidBockerName('123'), isFalse);
    expect(isValidBockerName(''), isFalse);
    expect(isValidBockerName('a' * 64), isFalse);
    expect(isValidBockerName('a b'), isFalse);
  });

  test('CommandResult summary 压缩长输出', () {
    final result = CommandResult(true, List.filled(100, 'abc').join(' '), 0);
    expect(result.summary.length, 163);
    expect(result.summary.endsWith('...'), isTrue);
  });
}
