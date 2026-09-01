import 'dart:convert';

/// 远程命令执行结果。
class CommandResult {
  const CommandResult(this.ok, this.output, this.exitCode);

  final bool ok;
  final String output;
  final int exitCode;

  /// 输出压缩成单行摘要（用于提示条展示）。
  String get summary {
    final condensed = output.replaceAll(RegExp(r'\s+'), ' ').trim();
    if (condensed.length <= 160) return condensed;
    return '${condensed.substring(0, 160)}...';
  }
}

/// 容器信息（对应 bocker container list --json）。
class ContainerInfo {
  const ContainerInfo({
    required this.name,
    required this.status,
    required this.network,
    required this.ipv4,
    required this.ipv6,
    required this.memory,
    required this.domain,
    required this.autostart,
    required this.ports,
  });

  final String name;
  final String status;
  final String network;
  final String ipv4;
  final String ipv6;
  final String memory;
  final String domain;
  final String autostart;
  final String ports;

  bool get isRunning => status.toLowerCase() == 'running';

  List<String> get portList => ports.isEmpty
      ? const []
      : ports
            .split(',')
            .map((item) => item.trim())
            .where((item) => item.isNotEmpty)
            .toList();
}

/// 本地镜像信息（对应 bocker image list --json）。
class ImageInfo {
  const ImageInfo({
    required this.name,
    required this.size,
    required this.created,
    required this.fingerprint,
  });

  final String name;
  final String size;
  final String created;
  final String fingerprint;
}

/// 远程模板（对应 bocker template list --json）。
class ImageTemplate {
  const ImageTemplate({
    required this.distro,
    required this.release,
    required this.image,
  });

  final String distro;
  final String release;
  final String image;
}

/// 容器挂载（对应 `bocker container set <name> mount list --json`）。
class MountInfo {
  const MountInfo({
    required this.name,
    required this.source,
    required this.target,
    required this.readonly,
    required this.inherited,
  });

  final String name;
  final String source;
  final String target;
  final bool readonly;
  final bool inherited;

  String get modeLabel => readonly ? 'ro' : 'rw';

  String get displayLabel =>
      '$source -> $target ($modeLabel)${inherited ? ' [镜像继承]' : ''}';
}

List<ContainerInfo> parseContainers(String output) {
  try {
    final decoded = jsonDecode(output);
    if (decoded is! List) return const [];
    return decoded.whereType<Map>().map((item) {
      return ContainerInfo(
        name: jsonText(item, 'name'),
        status: jsonText(item, 'status'),
        network: jsonText(item, 'network'),
        ipv4: jsonText(item, 'ipv4'),
        ipv6: jsonText(item, 'ipv6'),
        memory: jsonText(item, 'memory'),
        domain: jsonText(item, 'domain'),
        autostart: jsonText(item, 'autostart'),
        ports: jsonText(item, 'ports'),
      );
    }).toList();
  } catch (_) {
    return const [];
  }
}

List<ImageInfo> parseImages(String output) {
  try {
    final decoded = jsonDecode(output);
    if (decoded is! List) return const [];
    return decoded.whereType<Map>().map((item) {
      return ImageInfo(
        name: jsonText(item, 'name'),
        size: jsonText(item, 'size'),
        created: jsonText(item, 'created'),
        fingerprint: jsonText(item, 'fingerprint'),
      );
    }).toList();
  } catch (_) {
    return const [];
  }
}

List<ImageTemplate> parseImageTemplates(String output) {
  try {
    final decoded = jsonDecode(output);
    if (decoded is! List) return const [];
    return decoded.whereType<Map>().map((item) {
      return ImageTemplate(
        distro: jsonText(item, 'distro'),
        release: jsonText(item, 'release'),
        image: jsonText(item, 'image'),
      );
    }).toList();
  } catch (_) {
    return const [];
  }
}

List<MountInfo> parseMounts(String output) {
  try {
    final decoded = jsonDecode(output);
    if (decoded is! List) return const [];
    return decoded.whereType<Map>().map((item) {
      return MountInfo(
        name: jsonText(item, 'name'),
        source: jsonText(item, 'source'),
        target: jsonText(item, 'target'),
        readonly: item['readonly'] == true,
        inherited: item['inherited'] == true,
      );
    }).toList();
  } catch (_) {
    return const [];
  }
}

String jsonText(Map item, String key) => item[key]?.toString() ?? '';

/// 校验 bocker 资源命名规则：1-63 位字母数字与连字符，
/// 不能是纯数字，不能以连字符开头或结尾。
bool isValidBockerName(String name) {
  if (name.isEmpty || name.length > 63 || RegExp(r'^\d+$').hasMatch(name)) {
    return false;
  }
  return RegExp(
    r'^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$',
  ).hasMatch(name);
}
