import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:file_selector/file_selector.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

void main() {
  runApp(const BockerGuiApp());
}

class BockerGuiApp extends StatelessWidget {
  const BockerGuiApp({super.key});

  @override
  Widget build(BuildContext context) {
    const seed = Color(0xff0b57d0);
    return MaterialApp(
      title: 'Bocker',
      debugShowCheckedModeBanner: false,
      theme: _bockerTheme(
        ColorScheme.fromSeed(
          seedColor: seed,
          dynamicSchemeVariant: DynamicSchemeVariant.tonalSpot,
        ),
      ),
      darkTheme: _bockerTheme(
        ColorScheme.fromSeed(
          seedColor: seed,
          brightness: Brightness.dark,
          dynamicSchemeVariant: DynamicSchemeVariant.tonalSpot,
        ),
      ),
      themeMode: ThemeMode.system,
      home: const BockerHome(),
    );
  }
}

ThemeData _bockerTheme(ColorScheme colors) {
  final light = colors.brightness == Brightness.light;
  return ThemeData(
    colorScheme: colors,
    useMaterial3: true,
    scaffoldBackgroundColor: light ? const Color(0xfff8f9fa) : colors.surface,
    appBarTheme: AppBarTheme(
      centerTitle: false,
      elevation: 0,
      scrolledUnderElevation: 0,
      backgroundColor: light ? const Color(0xffffffff) : colors.surface,
      surfaceTintColor: Colors.transparent,
    ),
    cardTheme: CardThemeData(
      elevation: 0,
      margin: EdgeInsets.zero,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(8),
        side: BorderSide(color: colors.outlineVariant),
      ),
    ),
    dialogTheme: DialogThemeData(
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
    ),
    inputDecorationTheme: InputDecorationTheme(
      border: const OutlineInputBorder(),
      enabledBorder: OutlineInputBorder(
        borderSide: BorderSide(color: colors.outlineVariant),
      ),
      filled: true,
      fillColor: colors.surfaceContainerLowest,
      isDense: true,
    ),
    navigationRailTheme: NavigationRailThemeData(
      backgroundColor: light ? const Color(0xffffffff) : colors.surface,
      indicatorColor: colors.secondaryContainer,
      useIndicator: true,
    ),
    dataTableTheme: DataTableThemeData(
      headingRowColor: WidgetStatePropertyAll(colors.surfaceContainerLow),
      dividerThickness: 1,
    ),
    dividerTheme: DividerThemeData(color: colors.outlineVariant, space: 1),
    tooltipTheme: TooltipThemeData(
      waitDuration: const Duration(milliseconds: 450),
      decoration: BoxDecoration(
        color: colors.inverseSurface,
        borderRadius: BorderRadius.circular(4),
      ),
      textStyle: TextStyle(color: colors.onInverseSurface),
    ),
  );
}

enum _Section { containers, images }

class BockerHome extends StatefulWidget {
  const BockerHome({super.key, this.command});

  final BockerCommand? command;

  @override
  State<BockerHome> createState() => _BockerHomeState();
}

class _BockerHomeState extends State<BockerHome> {
  late final BockerCommand _bocker;
  _Section _section = _Section.containers;
  List<ContainerInfo> _containers = const [];
  List<ImageInfo> _images = const [];
  bool _loading = false;
  String? _busyLabel;
  String? _loadError;
  String _lastOutput = '';
  int _refreshGeneration = 0;

  @override
  void initState() {
    super.initState();
    _bocker = widget.command ?? BockerCommand();
    _refresh();
  }

  Future<void> _refresh() async {
    final generation = ++_refreshGeneration;
    final section = _section;
    setState(() {
      _loading = true;
      _busyLabel = '正在刷新';
      _loadError = null;
    });
    final result = await _bocker.run(
      section == _Section.containers
          ? ['container', 'list', '--json']
          : ['image', 'list', '--json'],
    );
    if (!mounted || generation != _refreshGeneration || section != _section) {
      return;
    }
    setState(() {
      _loading = false;
      _busyLabel = null;
      _lastOutput = result.output;
      if (result.ok) {
        _loadError = null;
        if (section == _Section.containers) {
          _containers = parseContainers(result.output);
        } else {
          _images = parseImages(result.output);
        }
      } else {
        _loadError = result.output;
      }
    });
  }

  Future<CommandResult> _run(
    String label,
    List<String> arguments, {
    bool refresh = true,
    String? workingDirectory,
  }) async {
    setState(() {
      _loading = true;
      _busyLabel = label;
    });
    final result = await _bocker.run(
      arguments,
      workingDirectory: workingDirectory,
      onOutput: (output) {
        if (mounted) setState(() => _lastOutput = output);
      },
    );
    if (!mounted) return result;
    setState(() {
      _loading = false;
      _busyLabel = null;
      _lastOutput = result.output;
    });
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(
          result.ok ? '$label 已完成' : '$label 失败: ${result.summary}',
        ),
        behavior: SnackBarBehavior.floating,
        backgroundColor: result.ok ? null : Theme.of(context).colorScheme.error,
      ),
    );
    if (result.ok && refresh) await _refresh();
    return result;
  }

  void _changeSection(_Section section) {
    if (section == _section) return;
    setState(() => _section = section);
    _refresh();
  }

  Future<List<ImageTemplate>?> _loadImageTemplates() async {
    setState(() {
      _loading = true;
      _busyLabel = '正在加载模板';
    });
    final result = await _bocker.run(['template', 'list', '--json']);
    if (!mounted) return null;
    setState(() {
      _loading = false;
      _busyLabel = null;
      _lastOutput = result.output;
    });
    if (!result.ok) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text('获取模板失败: ${result.summary}'),
          behavior: SnackBarBehavior.floating,
          backgroundColor: Theme.of(context).colorScheme.error,
        ),
      );
      return null;
    }
    final templates = parseImageTemplates(result.output);
    if (templates.isEmpty) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(
          content: Text('镜像源没有返回可用模板。'),
          behavior: SnackBarBehavior.floating,
        ),
      );
      return null;
    }
    return templates;
  }

  void _showInputError(String message) {
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(message),
        behavior: SnackBarBehavior.floating,
        backgroundColor: Theme.of(context).colorScheme.error,
      ),
    );
  }

  Future<String?> _selectFile({bool backup = false}) async {
    final groups = backup
        ? const [
            XTypeGroup(label: 'Bocker 备份', extensions: ['gz']),
          ]
        : const <XTypeGroup>[];
    final file = await openFile(acceptedTypeGroups: groups);
    return file?.path;
  }

  Future<void> _installDialog() async {
    final templates = await _loadImageTemplates();
    if (templates == null || !mounted) return;
    final name = TextEditingController();
    var network = 'nat';
    var permission = 'normal';
    var distro = templates.first.distro;
    var template = templates.first;
    final distros = templates.map((item) => item.distro).toSet().toList();
    final values = await showDialog<_InstallValues>(
      context: context,
      builder: (context) => StatefulBuilder(
        builder: (context, setDialogState) => AlertDialog(
          title: const Text('安装容器'),
          content: SingleChildScrollView(
            child: SizedBox(
              width: 440,
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  DropdownButtonFormField<String>(
                    initialValue: distro,
                    autofocus: true,
                    decoration: const InputDecoration(labelText: '发行版'),
                    items: distros
                        .map(
                          (item) =>
                              DropdownMenuItem(value: item, child: Text(item)),
                        )
                        .toList(),
                    onChanged: (value) => setDialogState(() {
                      distro = value!;
                      template = templates.firstWhere(
                        (item) => item.distro == distro,
                      );
                    }),
                  ),
                  const SizedBox(height: 16),
                  DropdownButtonFormField<String>(
                    key: ValueKey(distro),
                    initialValue: template.image,
                    decoration: const InputDecoration(labelText: '版本'),
                    items: templates
                        .where((item) => item.distro == distro)
                        .map(
                          (item) => DropdownMenuItem(
                            value: item.image,
                            child: Text('${item.release}  (${item.image})'),
                          ),
                        )
                        .toList(),
                    onChanged: (value) => setDialogState(() {
                      template = templates.firstWhere(
                        (item) => item.image == value,
                      );
                    }),
                  ),
                  const SizedBox(height: 16),
                  TextField(
                    controller: name,
                    decoration: const InputDecoration(
                      labelText: '容器名称',
                      hintText: '留空则由 Bocker 自动生成',
                    ),
                  ),
                  const SizedBox(height: 16),
                  _ChoiceSection(
                    label: '网络模式',
                    child: _NetworkSelector(
                      value: network,
                      onChanged: (value) =>
                          setDialogState(() => network = value),
                    ),
                  ),
                  const SizedBox(height: 16),
                  _ChoiceSection(
                    label: '容器权限',
                    child: _PermissionSelector(
                      value: permission,
                      onChanged: (value) =>
                          setDialogState(() => permission = value),
                    ),
                  ),
                ],
              ),
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context),
              child: const Text('取消'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(
                context,
                _InstallValues(
                  template.image,
                  name.text.trim(),
                  network,
                  permission,
                ),
              ),
              child: const Text('安装'),
            ),
          ],
        ),
      ),
    );
    name.dispose();
    if (values == null) return;
    if (values.name.isNotEmpty && !isValidBockerName(values.name)) {
      _showInputError('容器名称需为 1-63 位小写字母、数字或连字符，且不能以连字符开头或结尾。');
      return;
    }
    final args = templateInstallArguments(
      image: values.image,
      name: values.name,
      network: values.network,
      permission: values.permission,
    );
    await _run('安装容器', args);
  }

  Future<void> _buildDialog() async {
    final path = TextEditingController(text: 'Incusfile');
    final name = TextEditingController();
    var network = 'nat';
    final values = await showDialog<_BuildValues>(
      context: context,
      builder: (context) => StatefulBuilder(
        builder: (context, setDialogState) => AlertDialog(
          title: const Text('构建镜像'),
          content: SingleChildScrollView(
            child: SizedBox(
              width: 440,
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  TextField(
                    controller: path,
                    decoration: InputDecoration(
                      labelText: 'Incusfile 路径',
                      suffixIcon: IconButton(
                        tooltip: '选择 Incusfile',
                        onPressed: () async {
                          final selected = await _selectFile();
                          if (selected != null) {
                            path.text = selected;
                            setDialogState(() {});
                          }
                        },
                        icon: const Icon(Icons.folder_open_outlined),
                      ),
                    ),
                  ),
                  const SizedBox(height: 16),
                  TextField(
                    controller: name,
                    decoration: const InputDecoration(labelText: '镜像别名（可选）'),
                  ),
                  const SizedBox(height: 16),
                  _ChoiceSection(
                    label: '构建网络',
                    child: _NetworkSelector(
                      value: network,
                      onChanged: (value) =>
                          setDialogState(() => network = value),
                    ),
                  ),
                ],
              ),
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context),
              child: const Text('取消'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(
                context,
                _BuildValues(path.text.trim(), name.text.trim(), network),
              ),
              child: const Text('构建'),
            ),
          ],
        ),
      ),
    );
    path.dispose();
    name.dispose();
    if (values == null || values.path.isEmpty) return;
    if (!File(values.path).existsSync()) {
      _showInputError('找不到 Incusfile：${values.path}');
      return;
    }
    if (values.name.isNotEmpty && !isValidBockerName(values.name)) {
      _showInputError('镜像别名需为 1-63 位小写字母、数字或连字符。');
      return;
    }
    await _run('构建镜像', [
      'image',
      'build',
      '--network',
      values.network,
      if (values.name.isNotEmpty) ...['--name', values.name],
      values.path,
    ]);
  }

  Future<void> _runImageDialog(ImageInfo image) async {
    final defaultName = image.name.length <= 61
        ? '${image.name}-1'
        : '${image.name.substring(0, 61)}-1';
    final name = TextEditingController(text: defaultName);
    var network = 'default';
    var permission = 'normal';
    final values = await showDialog<_RunImageValues>(
      context: context,
      builder: (context) => StatefulBuilder(
        builder: (context, setDialogState) => AlertDialog(
          title: Text('运行镜像 ${image.name}'),
          content: SingleChildScrollView(
            child: SizedBox(
              width: 440,
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  TextField(
                    controller: name,
                    autofocus: true,
                    decoration: const InputDecoration(labelText: '容器名称'),
                  ),
                  const SizedBox(height: 16),
                  _ChoiceSection(
                    label: '网络模式',
                    child: SegmentedButton<String>(
                      showSelectedIcon: false,
                      segments: const [
                        ButtonSegment(value: 'default', label: Text('镜像设置')),
                        ButtonSegment(value: 'nat', label: Text('NAT')),
                        ButtonSegment(value: 'bridge', label: Text('Bridge')),
                      ],
                      selected: {network},
                      onSelectionChanged: (value) =>
                          setDialogState(() => network = value.first),
                    ),
                  ),
                  const SizedBox(height: 16),
                  _ChoiceSection(
                    label: '容器权限',
                    child: _PermissionSelector(
                      value: permission,
                      onChanged: (value) =>
                          setDialogState(() => permission = value),
                    ),
                  ),
                ],
              ),
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context),
              child: const Text('取消'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(
                context,
                _RunImageValues(name.text.trim(), network, permission),
              ),
              child: const Text('创建并启动'),
            ),
          ],
        ),
      ),
    );
    name.dispose();
    if (values == null) return;
    if (!isValidBockerName(values.name)) {
      if (!mounted) return;
      _showInputError('容器名称需为 1-63 位小写字母、数字或连字符。');
      return;
    }
    await _run(
      '运行镜像',
      imageRunArguments(
        image: image.name,
        name: values.name,
        network: values.network,
        permission: values.permission,
      ),
    );
  }

  Future<void> _importDialog() async {
    final path = TextEditingController();
    final name = TextEditingController();
    var network = 'nat';
    var permission = 'normal';
    final values = await showDialog<_ImportValues>(
      context: context,
      builder: (context) => StatefulBuilder(
        builder: (context, setDialogState) => AlertDialog(
          title: const Text('导入备份'),
          content: SingleChildScrollView(
            child: SizedBox(
              width: 440,
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  TextField(
                    controller: path,
                    autofocus: true,
                    decoration: InputDecoration(
                      labelText: '备份文件路径',
                      suffixIcon: IconButton(
                        tooltip: '选择备份文件',
                        onPressed: () async {
                          final selected = await _selectFile(backup: true);
                          if (selected != null) {
                            path.text = selected;
                            setDialogState(() {});
                          }
                        },
                        icon: const Icon(Icons.folder_open_outlined),
                      ),
                    ),
                  ),
                  const SizedBox(height: 16),
                  TextField(
                    controller: name,
                    decoration: const InputDecoration(labelText: '容器名称（可选）'),
                  ),
                  const SizedBox(height: 16),
                  _ChoiceSection(
                    label: '网络模式',
                    child: _NetworkSelector(
                      value: network,
                      onChanged: (value) =>
                          setDialogState(() => network = value),
                    ),
                  ),
                  const SizedBox(height: 16),
                  _ChoiceSection(
                    label: '容器权限',
                    child: _PermissionSelector(
                      value: permission,
                      onChanged: (value) =>
                          setDialogState(() => permission = value),
                    ),
                  ),
                ],
              ),
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context),
              child: const Text('取消'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(
                context,
                _ImportValues(
                  path.text.trim(),
                  name.text.trim(),
                  network,
                  permission,
                ),
              ),
              child: const Text('导入'),
            ),
          ],
        ),
      ),
    );
    path.dispose();
    name.dispose();
    if (values == null || values.path.isEmpty) return;
    if (!File(values.path).existsSync()) {
      _showInputError('找不到备份文件：${values.path}');
      return;
    }
    if (values.name.isNotEmpty && !isValidBockerName(values.name)) {
      _showInputError('容器名称需为 1-63 位小写字母、数字或连字符。');
      return;
    }
    await _run('导入备份', [
      'container',
      'import',
      values.path,
      if (values.name.isNotEmpty) values.name,
      '--network',
      values.network,
      '--permission',
      values.permission,
    ]);
  }

  Future<void> _execDialog(ContainerInfo container) async {
    final command = TextEditingController(text: 'uname -a');
    final value = await showDialog<String>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text('在 ${container.name} 中执行命令'),
        content: SizedBox(
          width: 520,
          child: TextField(
            controller: command,
            autofocus: true,
            minLines: 2,
            maxLines: 5,
            decoration: const InputDecoration(labelText: '命令'),
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, command.text.trim()),
            child: const Text('执行'),
          ),
        ],
      ),
    );
    command.dispose();
    if (value == null || value.isEmpty) return;
    final words = splitShellWords(value);
    if (words.isEmpty) return;
    final result = await _run('执行命令', [
      'container',
      'exec',
      container.name,
      ...words,
    ], refresh: false);
    if (!mounted) return;
    await showDialog<void>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(result.ok ? '命令输出' : '命令失败'),
        content: SizedBox(
          width: 680,
          child: SelectableText(
            result.output.isEmpty ? '命令没有输出。' : result.output,
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('关闭'),
          ),
        ],
      ),
    );
  }

  Future<void> _openContainerShell(ContainerInfo container) async {
    final result = await _bocker.openShell(container.name);
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(
          result.ok
              ? '已在系统终端打开 ${container.name}'
              : '无法打开终端: ${result.summary}',
        ),
        behavior: SnackBarBehavior.floating,
        backgroundColor: result.ok ? null : Theme.of(context).colorScheme.error,
      ),
    );
  }

  Future<void> _settingsDialog(ContainerInfo container) async {
    final domain = TextEditingController(text: container.domain);
    final port = TextEditingController();
    final removablePorts = removablePortSpecs(container.ports);
    var removePort = '';
    var autostart = container.autostart == 'on';
    var network = container.network == 'bridge' ? 'bridge' : 'nat';
    final values = await showDialog<_SettingsValues>(
      context: context,
      builder: (context) => StatefulBuilder(
        builder: (context, setDialogState) => AlertDialog(
          title: Text('${container.name} 设置'),
          content: SingleChildScrollView(
            child: SizedBox(
              width: 480,
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text('访问', style: Theme.of(context).textTheme.titleSmall),
                  const SizedBox(height: 10),
                  TextField(
                    controller: domain,
                    decoration: const InputDecoration(
                      labelText: '域名映射',
                      hintText: '例如 app.test；留空可取消映射',
                    ),
                  ),
                  const SizedBox(height: 16),
                  TextField(
                    controller: port,
                    decoration: const InputDecoration(
                      labelText: '新增端口映射',
                      hintText: '例如 8080:80/tcp',
                    ),
                  ),
                  if (removablePorts.isNotEmpty) ...[
                    const SizedBox(height: 16),
                    DropdownButtonFormField<String>(
                      initialValue: null,
                      decoration: const InputDecoration(labelText: '删除已有端口映射'),
                      items: removablePorts
                          .map(
                            (item) => DropdownMenuItem(
                              value: item,
                              child: Text(item),
                            ),
                          )
                          .toList(),
                      onChanged: (value) =>
                          setDialogState(() => removePort = value ?? ''),
                    ),
                  ],
                  const SizedBox(height: 20),
                  Divider(color: Theme.of(context).colorScheme.outlineVariant),
                  const SizedBox(height: 16),
                  Text('运行', style: Theme.of(context).textTheme.titleSmall),
                  const SizedBox(height: 4),
                  SwitchListTile(
                    contentPadding: EdgeInsets.zero,
                    title: const Text('开机自启动'),
                    value: autostart,
                    onChanged: (value) =>
                        setDialogState(() => autostart = value),
                  ),
                  const SizedBox(height: 8),
                  _ChoiceSection(
                    label: '网络模式',
                    supportingText: container.isRunning
                        ? '停止容器后才能切换网络模式'
                        : null,
                    child: _NetworkSelector(
                      value: network,
                      enabled: !container.isRunning,
                      onChanged: (value) =>
                          setDialogState(() => network = value),
                    ),
                  ),
                ],
              ),
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context),
              child: const Text('取消'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(
                context,
                _SettingsValues(
                  domain.text.trim(),
                  port.text.trim(),
                  removePort,
                  autostart,
                  network,
                ),
              ),
              child: const Text('保存'),
            ),
          ],
        ),
      ),
    );
    domain.dispose();
    port.dispose();
    if (values == null) return;
    if (values.domain.isNotEmpty &&
        values.domain != '-' &&
        !isValidDomain(values.domain)) {
      _showInputError('域名格式无效，例如可填写 app.test。');
      return;
    }
    if (values.port.isNotEmpty && !isValidPortMapping(values.port)) {
      _showInputError('端口映射格式无效，例如 8080:80/tcp。');
      return;
    }
    var ok = true;
    if (values.domain != container.domain) {
      ok = (await _run('保存域名', [
        'container',
        'set',
        container.name,
        'domain',
        values.domain.isEmpty || values.domain == '-'
            ? '--unset'
            : values.domain,
      ], refresh: false)).ok;
    }
    if (ok && values.port.isNotEmpty) {
      ok = (await _run('添加端口映射', [
        'container',
        'set',
        container.name,
        'port',
        values.port,
      ], refresh: false)).ok;
    }
    if (ok && values.removePort.isNotEmpty) {
      ok = (await _run('删除端口映射', [
        'container',
        'set',
        container.name,
        'port',
        'rm',
        values.removePort,
      ], refresh: false)).ok;
    }
    if (ok && values.autostart != (container.autostart == 'on')) {
      ok = (await _run('保存自启动', [
        'container',
        'set',
        container.name,
        'autostart',
        values.autostart ? 'on' : 'off',
      ], refresh: false)).ok;
    }
    if (ok && !container.isRunning && values.network != container.network) {
      await _run('保存网络模式', [
        'container',
        'set',
        container.name,
        'network',
        values.network,
      ], refresh: false);
    }
    if (mounted) await _refresh();
  }

  Future<void> _deleteContainer(ContainerInfo container) async {
    final confirmed = await _confirm(
      '删除容器',
      '删除 ${container.name} 及其数据？此操作无法撤销。',
      confirmLabel: '删除',
    );
    if (confirmed) {
      await _run('删除容器', ['container', 'remove', container.name]);
    }
  }

  Future<void> _deleteImage(ImageInfo image) async {
    final confirmed = await _confirm(
      '删除镜像',
      '删除镜像别名 ${image.name}？此操作无法撤销。',
      confirmLabel: '删除',
    );
    if (confirmed) await _run('删除镜像', ['image', 'remove', image.name]);
  }

  Future<bool> _confirm(
    String title,
    String message, {
    required String confirmLabel,
  }) async {
    return await showDialog<bool>(
          context: context,
          builder: (context) => AlertDialog(
            title: Text(title),
            content: Text(message),
            actions: [
              TextButton(
                onPressed: () => Navigator.pop(context, false),
                child: const Text('取消'),
              ),
              FilledButton(
                onPressed: () => Navigator.pop(context, true),
                style: FilledButton.styleFrom(
                  backgroundColor: Theme.of(context).colorScheme.error,
                  foregroundColor: Theme.of(context).colorScheme.onError,
                ),
                child: Text(confirmLabel),
              ),
            ],
          ),
        ) ??
        false;
  }

  void _showOutput() {
    showDialog<void>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('最近命令输出'),
        content: SizedBox(
          width: 720,
          child: SelectableText(_lastOutput.isEmpty ? '尚未执行命令。' : _lastOutput),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('关闭'),
          ),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final wide = MediaQuery.sizeOf(context).width >= 900;
    final body = _section == _Section.containers
        ? _ContainersView(
            containers: _containers,
            loading: _loading,
            error: _loadError,
            onRetry: _refresh,
            onInstall: _installDialog,
            onBuild: _buildDialog,
            onImport: _importDialog,
            onStart: (item) => _run('启动容器', ['container', 'start', item.name]),
            onStop: (item) => _run('停止容器', ['container', 'stop', item.name]),
            onRestart: (item) =>
                _run('重启容器', ['container', 'restart', item.name]),
            onExport: (item) => _run('导出容器', [
              'container',
              'export',
              item.name,
            ], workingDirectory: _bocker.userHomeDirectory),
            onOpenShell: _openContainerShell,
            onExec: _execDialog,
            onSettings: _settingsDialog,
            onDelete: _deleteContainer,
          )
        : _ImagesView(
            images: _images,
            loading: _loading,
            error: _loadError,
            onRetry: _refresh,
            onBuild: _buildDialog,
            onRun: _runImageDialog,
            onDelete: _deleteImage,
          );
    final rail = NavigationRail(
      selectedIndex: _section.index,
      labelType: NavigationRailLabelType.all,
      onDestinationSelected: (index) => _changeSection(_Section.values[index]),
      destinations: const [
        NavigationRailDestination(
          icon: Icon(Icons.inventory_2_outlined),
          selectedIcon: Icon(Icons.inventory_2),
          label: Text('容器'),
        ),
        NavigationRailDestination(
          icon: Icon(Icons.layers_outlined),
          selectedIcon: Icon(Icons.layers),
          label: Text('镜像'),
        ),
      ],
    );
    return Scaffold(
      appBar: AppBar(
        title: const Row(
          children: [
            Icon(Icons.developer_board),
            SizedBox(width: 10),
            Text('Bocker'),
          ],
        ),
        actions: [
          IconButton(
            tooltip: '查看命令输出',
            onPressed: _showOutput,
            icon: const Icon(Icons.terminal),
          ),
          IconButton(
            tooltip: '刷新',
            onPressed: _loading ? null : _refresh,
            icon: const Icon(Icons.refresh),
          ),
          const SizedBox(width: 8),
        ],
      ),
      drawer: wide
          ? null
          : NavigationDrawer(
              selectedIndex: _section.index,
              onDestinationSelected: (index) {
                Navigator.pop(context);
                _changeSection(_Section.values[index]);
              },
              children: const [
                Padding(
                  padding: EdgeInsets.fromLTRB(28, 24, 16, 14),
                  child: Text('管理'),
                ),
                NavigationDrawerDestination(
                  icon: Icon(Icons.inventory_2_outlined),
                  selectedIcon: Icon(Icons.inventory_2),
                  label: Text('容器'),
                ),
                NavigationDrawerDestination(
                  icon: Icon(Icons.layers_outlined),
                  selectedIcon: Icon(Icons.layers),
                  label: Text('镜像'),
                ),
              ],
            ),
      body: Column(
        children: [
          if (_loading)
            Container(
              height: 36,
              width: double.infinity,
              color: Theme.of(context).colorScheme.secondaryContainer,
              padding: const EdgeInsets.symmetric(horizontal: 18),
              child: Row(
                children: [
                  const SizedBox.square(
                    dimension: 16,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  ),
                  const SizedBox(width: 10),
                  Expanded(child: Text(_busyLabel ?? '正在处理')),
                  if (_busyLabel != '正在刷新' && _busyLabel != '正在加载模板')
                    IconButton(
                      tooltip: '取消当前操作',
                      onPressed: () {
                        _bocker.cancelActive();
                        setState(() => _busyLabel = '正在取消');
                      },
                      icon: const Icon(Icons.close),
                    ),
                ],
              ),
            ),
          Expanded(
            child: Row(
              children: [
                if (wide) rail,
                if (wide) const VerticalDivider(width: 1),
                Expanded(child: body),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _ChoiceSection extends StatelessWidget {
  const _ChoiceSection({
    required this.label,
    required this.child,
    this.supportingText,
  });

  final String label;
  final String? supportingText;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text(label, style: Theme.of(context).textTheme.labelLarge),
        const SizedBox(height: 8),
        child,
        if (supportingText != null) ...[
          const SizedBox(height: 6),
          Text(
            supportingText!,
            style: Theme.of(context).textTheme.bodySmall?.copyWith(
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
        ],
      ],
    );
  }
}

class _NetworkSelector extends StatelessWidget {
  const _NetworkSelector({
    required this.value,
    required this.onChanged,
    this.enabled = true,
  });

  final String value;
  final ValueChanged<String> onChanged;
  final bool enabled;

  @override
  Widget build(BuildContext context) {
    return SegmentedButton<String>(
      showSelectedIcon: false,
      expandedInsets: EdgeInsets.zero,
      segments: const [
        ButtonSegment(
          value: 'nat',
          icon: Icon(Icons.public),
          label: Text('NAT'),
        ),
        ButtonSegment(
          value: 'bridge',
          icon: Icon(Icons.lan_outlined),
          label: Text('Bridge'),
        ),
      ],
      selected: {value},
      onSelectionChanged: enabled
          ? (selected) => onChanged(selected.first)
          : null,
    );
  }
}

class _PermissionSelector extends StatelessWidget {
  const _PermissionSelector({required this.value, required this.onChanged});

  final String value;
  final ValueChanged<String> onChanged;

  @override
  Widget build(BuildContext context) {
    return SegmentedButton<String>(
      showSelectedIcon: false,
      expandedInsets: EdgeInsets.zero,
      segments: const [
        ButtonSegment(
          value: 'normal',
          icon: Icon(Icons.shield_outlined),
          label: Text('普通'),
        ),
        ButtonSegment(
          value: 'super',
          icon: Icon(Icons.admin_panel_settings_outlined),
          label: Text('超级'),
        ),
      ],
      selected: {value},
      onSelectionChanged: (selected) => onChanged(selected.first),
    );
  }
}

class _ContainersView extends StatelessWidget {
  const _ContainersView({
    required this.containers,
    required this.loading,
    required this.error,
    required this.onRetry,
    required this.onInstall,
    required this.onBuild,
    required this.onImport,
    required this.onStart,
    required this.onStop,
    required this.onRestart,
    required this.onExport,
    required this.onOpenShell,
    required this.onExec,
    required this.onSettings,
    required this.onDelete,
  });

  final List<ContainerInfo> containers;
  final bool loading;
  final String? error;
  final VoidCallback onRetry;
  final VoidCallback onInstall;
  final VoidCallback onBuild;
  final VoidCallback onImport;
  final ValueChanged<ContainerInfo> onStart;
  final ValueChanged<ContainerInfo> onStop;
  final ValueChanged<ContainerInfo> onRestart;
  final ValueChanged<ContainerInfo> onExport;
  final ValueChanged<ContainerInfo> onOpenShell;
  final ValueChanged<ContainerInfo> onExec;
  final ValueChanged<ContainerInfo> onSettings;
  final ValueChanged<ContainerInfo> onDelete;

  @override
  Widget build(BuildContext context) {
    return _PageFrame(
      title: '容器',
      subtitle: '${containers.length} 个容器',
      primary: FilledButton.icon(
        onPressed: loading ? null : onInstall,
        icon: const Icon(Icons.add),
        label: const Text('安装容器'),
      ),
      actions: [
        IconButton(
          tooltip: '构建镜像',
          onPressed: loading ? null : onBuild,
          icon: const Icon(Icons.build_outlined),
        ),
        IconButton(
          tooltip: '导入备份',
          onPressed: loading ? null : onImport,
          icon: const Icon(Icons.file_upload_outlined),
        ),
      ],
      child: _ContentState(
        loading: loading,
        error: error,
        onRetry: onRetry,
        empty: containers.isEmpty,
        emptyIcon: Icons.inventory_2_outlined,
        emptyText: '还没有容器',
        child: LayoutBuilder(
          builder: (context, constraints) {
            if (constraints.maxWidth < 1180) {
              return ListView.separated(
                padding: const EdgeInsets.only(bottom: 12),
                itemCount: containers.length,
                separatorBuilder: (context, index) =>
                    const SizedBox(height: 10),
                itemBuilder: (context, index) => _ContainerCard(
                  container: containers[index],
                  loading: loading,
                  onStart: onStart,
                  onStop: onStop,
                  onRestart: onRestart,
                  onExport: onExport,
                  onOpenShell: onOpenShell,
                  onExec: onExec,
                  onSettings: onSettings,
                  onDelete: onDelete,
                ),
              );
            }
            return SingleChildScrollView(
              scrollDirection: Axis.horizontal,
              child: ConstrainedBox(
                constraints: BoxConstraints(minWidth: constraints.maxWidth),
                child: DataTable(
                  columnSpacing: 20,
                  dataRowMinHeight: 56,
                  dataRowMaxHeight: 160,
                  columns: const [
                    DataColumn(label: Text('名称')),
                    DataColumn(label: Text('状态')),
                    DataColumn(label: Text('网络')),
                    DataColumn(label: Text('IPv4')),
                    DataColumn(label: Text('IPv6')),
                    DataColumn(label: Text('域名')),
                    DataColumn(label: Text('端口')),
                    DataColumn(label: Text('自启动')),
                    DataColumn(label: Text('操作')),
                  ],
                  rows: containers
                      .map(
                        (item) => DataRow(
                          cells: [
                            DataCell(
                              InkWell(
                                onTap: item.isRunning && !loading
                                    ? () => onOpenShell(item)
                                    : null,
                                child: Row(
                                  mainAxisSize: MainAxisSize.min,
                                  children: [
                                    Text(item.name),
                                    if (item.isRunning) ...[
                                      const SizedBox(width: 6),
                                      const Icon(Icons.terminal, size: 16),
                                    ],
                                  ],
                                ),
                              ),
                            ),
                            DataCell(
                              _StatusChip(
                                running: item.isRunning,
                                label: item.status,
                              ),
                            ),
                            DataCell(Text(item.network)),
                            DataCell(CopyableText(text: item.ipv4)),
                            DataCell(CopyableText(text: item.ipv6)),
                            DataCell(
                              ConstrainedBox(
                                constraints: const BoxConstraints(
                                  minWidth: 100,
                                  maxWidth: 180,
                                ),
                                child: CopyableText(text: item.domain),
                              ),
                            ),
                            DataCell(
                              Tooltip(
                                message: item.ports.isEmpty ? '-' : item.ports,
                                child: ConstrainedBox(
                                  constraints: const BoxConstraints(
                                    minWidth: 190,
                                    maxWidth: 250,
                                  ),
                                  child: Text(
                                    item.portsDisplay,
                                    softWrap: true,
                                  ),
                                ),
                              ),
                            ),
                            DataCell(
                              Icon(
                                item.autostart == 'on'
                                    ? Icons.check_circle_outline
                                    : Icons.remove_circle_outline,
                                size: 19,
                                color: item.autostart == 'on'
                                    ? Theme.of(context).colorScheme.primary
                                    : null,
                              ),
                            ),
                            DataCell(
                              Row(
                                mainAxisSize: MainAxisSize.min,
                                children: [
                                  if (item.isRunning) ...[
                                    IconButton(
                                      tooltip: '进入容器终端',
                                      onPressed: loading
                                          ? null
                                          : () => onOpenShell(item),
                                      icon: const Icon(Icons.terminal),
                                    ),
                                    IconButton(
                                      tooltip: '停止',
                                      onPressed: loading
                                          ? null
                                          : () => onStop(item),
                                      icon: const Icon(
                                        Icons.stop_circle_outlined,
                                      ),
                                    ),
                                    IconButton(
                                      tooltip: '重启',
                                      onPressed: loading
                                          ? null
                                          : () => onRestart(item),
                                      icon: const Icon(Icons.restart_alt),
                                    ),
                                  ] else
                                    IconButton(
                                      tooltip: '启动',
                                      onPressed: loading
                                          ? null
                                          : () => onStart(item),
                                      icon: const Icon(
                                        Icons.play_circle_outline,
                                      ),
                                    ),
                                  PopupMenuButton<_ContainerAction>(
                                    tooltip: '更多操作',
                                    onSelected: (action) {
                                      switch (action) {
                                        case _ContainerAction.exec:
                                          onExec(item);
                                        case _ContainerAction.settings:
                                          onSettings(item);
                                        case _ContainerAction.export:
                                          onExport(item);
                                        case _ContainerAction.delete:
                                          onDelete(item);
                                      }
                                    },
                                    itemBuilder: (context) => const [
                                      PopupMenuItem(
                                        value: _ContainerAction.exec,
                                        child: ListTile(
                                          leading: Icon(Icons.terminal),
                                          title: Text('执行命令'),
                                        ),
                                      ),
                                      PopupMenuItem(
                                        value: _ContainerAction.settings,
                                        child: ListTile(
                                          leading: Icon(Icons.tune),
                                          title: Text('设置'),
                                        ),
                                      ),
                                      PopupMenuItem(
                                        value: _ContainerAction.export,
                                        child: ListTile(
                                          leading: Icon(
                                            Icons.file_download_outlined,
                                          ),
                                          title: Text('导出备份'),
                                        ),
                                      ),
                                      PopupMenuDivider(),
                                      PopupMenuItem(
                                        value: _ContainerAction.delete,
                                        child: ListTile(
                                          leading: Icon(Icons.delete_outline),
                                          title: Text('删除'),
                                        ),
                                      ),
                                    ],
                                  ),
                                ],
                              ),
                            ),
                          ],
                        ),
                      )
                      .toList(),
                ),
              ),
            );
          },
        ),
      ),
    );
  }
}

class _ContainerCard extends StatelessWidget {
  const _ContainerCard({
    required this.container,
    required this.loading,
    required this.onStart,
    required this.onStop,
    required this.onRestart,
    required this.onExport,
    required this.onOpenShell,
    required this.onExec,
    required this.onSettings,
    required this.onDelete,
  });

  final ContainerInfo container;
  final bool loading;
  final ValueChanged<ContainerInfo> onStart;
  final ValueChanged<ContainerInfo> onStop;
  final ValueChanged<ContainerInfo> onRestart;
  final ValueChanged<ContainerInfo> onExport;
  final ValueChanged<ContainerInfo> onOpenShell;
  final ValueChanged<ContainerInfo> onExec;
  final ValueChanged<ContainerInfo> onSettings;
  final ValueChanged<ContainerInfo> onDelete;

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    return Card(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 14, 8, 12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(
                  child: Text(
                    container.name,
                    style: Theme.of(context).textTheme.titleMedium,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
                _StatusChip(
                  running: container.isRunning,
                  label: container.status,
                ),
              ],
            ),
            const SizedBox(height: 12),
            Wrap(
              spacing: 18,
              runSpacing: 10,
              children: [
                _ContainerDetail(
                  icon: Icons.lan_outlined,
                  label: container.network,
                ),
                _ContainerDetail(
                  icon: Icons.language,
                  label: container.ipv4.isEmpty ? '无 IPv4' : container.ipv4,
                ),
                if (container.ipv6.isNotEmpty)
                  _ContainerDetail(
                    icon: Icons.language_outlined,
                    label: container.ipv6,
                  ),
                if (container.domain.isNotEmpty)
                  _ContainerDetail(
                    icon: Icons.alternate_email,
                    label: container.domain,
                  ),
                if (container.ports.isNotEmpty && container.ports != '-')
                  _ContainerDetail(
                    icon: Icons.swap_horiz,
                    label: container.ports,
                  ),
              ],
            ),
            const SizedBox(height: 8),
            Row(
              mainAxisAlignment: MainAxisAlignment.end,
              children: [
                if (container.isRunning) ...[
                  IconButton(
                    tooltip: '进入容器终端',
                    onPressed: loading ? null : () => onOpenShell(container),
                    icon: const Icon(Icons.terminal),
                  ),
                  IconButton(
                    tooltip: '停止',
                    onPressed: loading ? null : () => onStop(container),
                    icon: const Icon(Icons.stop_circle_outlined),
                  ),
                  IconButton(
                    tooltip: '重启',
                    onPressed: loading ? null : () => onRestart(container),
                    icon: const Icon(Icons.restart_alt),
                  ),
                ] else
                  IconButton(
                    tooltip: '启动',
                    onPressed: loading ? null : () => onStart(container),
                    icon: const Icon(Icons.play_circle_outline),
                  ),
                PopupMenuButton<_ContainerAction>(
                  tooltip: '更多操作',
                  enabled: !loading,
                  onSelected: (action) {
                    switch (action) {
                      case _ContainerAction.exec:
                        onExec(container);
                      case _ContainerAction.settings:
                        onSettings(container);
                      case _ContainerAction.export:
                        onExport(container);
                      case _ContainerAction.delete:
                        onDelete(container);
                    }
                  },
                  itemBuilder: (context) => [
                    const PopupMenuItem(
                      value: _ContainerAction.exec,
                      child: ListTile(
                        leading: Icon(Icons.terminal),
                        title: Text('执行命令'),
                      ),
                    ),
                    const PopupMenuItem(
                      value: _ContainerAction.settings,
                      child: ListTile(
                        leading: Icon(Icons.tune),
                        title: Text('设置'),
                      ),
                    ),
                    const PopupMenuItem(
                      value: _ContainerAction.export,
                      child: ListTile(
                        leading: Icon(Icons.file_download_outlined),
                        title: Text('导出备份'),
                      ),
                    ),
                    const PopupMenuDivider(),
                    PopupMenuItem(
                      value: _ContainerAction.delete,
                      child: ListTile(
                        leading: Icon(
                          Icons.delete_outline,
                          color: colors.error,
                        ),
                        title: Text(
                          '删除',
                          style: TextStyle(color: colors.error),
                        ),
                      ),
                    ),
                  ],
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

class _ContainerDetail extends StatelessWidget {
  const _ContainerDetail({required this.icon, required this.label});

  final IconData icon;
  final String label;

  @override
  Widget build(BuildContext context) {
    return ConstrainedBox(
      constraints: const BoxConstraints(maxWidth: 300),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(
            icon,
            size: 17,
            color: Theme.of(context).colorScheme.onSurfaceVariant,
          ),
          const SizedBox(width: 6),
          Flexible(child: Text(label, overflow: TextOverflow.ellipsis)),
        ],
      ),
    );
  }
}

enum _ContainerAction { exec, settings, export, delete }

class CopyableText extends StatelessWidget {
  const CopyableText({required this.text, super.key});

  final String text;

  @override
  Widget build(BuildContext context) {
    if (text.isEmpty) return const Text('-');

    return Tooltip(
      message: '点击复制: $text',
      child: InkWell(
        mouseCursor: SystemMouseCursors.click,
        onTap: () async {
          await Clipboard.setData(ClipboardData(text: text));
          if (!context.mounted) return;
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: Text('已复制: $text'),
              behavior: SnackBarBehavior.floating,
              duration: const Duration(seconds: 2),
            ),
          );
        },
        child: Text(text, softWrap: true),
      ),
    );
  }
}

class _ImagesView extends StatelessWidget {
  const _ImagesView({
    required this.images,
    required this.loading,
    required this.error,
    required this.onRetry,
    required this.onBuild,
    required this.onRun,
    required this.onDelete,
  });

  final List<ImageInfo> images;
  final bool loading;
  final String? error;
  final VoidCallback onRetry;
  final VoidCallback onBuild;
  final ValueChanged<ImageInfo> onRun;
  final ValueChanged<ImageInfo> onDelete;

  @override
  Widget build(BuildContext context) {
    return _PageFrame(
      title: '镜像',
      subtitle: '${images.length} 个本地镜像',
      primary: FilledButton.icon(
        onPressed: loading ? null : onBuild,
        icon: const Icon(Icons.build_outlined),
        label: const Text('构建镜像'),
      ),
      child: _ContentState(
        loading: loading,
        error: error,
        onRetry: onRetry,
        empty: images.isEmpty,
        emptyIcon: Icons.layers_outlined,
        emptyText: '还没有本地镜像',
        child: ListView.separated(
          itemCount: images.length,
          separatorBuilder: (context, index) => const Divider(height: 1),
          itemBuilder: (context, index) {
            final item = images[index];
            return ListTile(
              leading: const Icon(Icons.layers_outlined),
              title: Text(item.name),
              subtitle: Text(
                '${item.size}  ${item.created}  ${item.fingerprint}',
              ),
              trailing: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  IconButton(
                    tooltip: '创建并启动容器',
                    onPressed: loading ? null : () => onRun(item),
                    icon: const Icon(Icons.play_arrow),
                  ),
                  IconButton(
                    tooltip: '删除镜像',
                    onPressed: loading ? null : () => onDelete(item),
                    icon: const Icon(Icons.delete_outline),
                  ),
                ],
              ),
            );
          },
        ),
      ),
    );
  }
}

class _PageFrame extends StatelessWidget {
  const _PageFrame({
    required this.title,
    required this.subtitle,
    required this.primary,
    this.actions = const [],
    required this.child,
  });

  final String title;
  final String subtitle;
  final Widget primary;
  final List<Widget> actions;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final compact = constraints.maxWidth < 620;
        final titleBlock = Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(title, style: Theme.of(context).textTheme.headlineSmall),
            Text(
              subtitle,
              style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                color: Theme.of(context).colorScheme.onSurfaceVariant,
              ),
            ),
          ],
        );
        final controls = Wrap(
          spacing: 8,
          runSpacing: 8,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: [primary, ...actions],
        );
        return Padding(
          padding: EdgeInsets.all(compact ? 16 : 24),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              if (compact) ...[
                titleBlock,
                const SizedBox(height: 14),
                controls,
              ] else
                Row(children: [titleBlock, const Spacer(), controls]),
              const SizedBox(height: 20),
              Expanded(child: child),
            ],
          ),
        );
      },
    );
  }
}

class _ContentState extends StatelessWidget {
  const _ContentState({
    required this.loading,
    required this.error,
    required this.onRetry,
    required this.empty,
    required this.emptyIcon,
    required this.emptyText,
    required this.child,
  });

  final bool loading;
  final String? error;
  final VoidCallback onRetry;
  final bool empty;
  final IconData emptyIcon;
  final String emptyText;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    if (loading && empty) {
      return const Center(child: CircularProgressIndicator());
    }
    if (error != null) {
      return Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 680),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(
                Icons.error_outline,
                size: 44,
                color: Theme.of(context).colorScheme.error,
              ),
              const SizedBox(height: 14),
              Text(
                '无法读取 Bocker 状态',
                style: Theme.of(context).textTheme.titleLarge,
              ),
              const SizedBox(height: 8),
              SelectableText(error!, textAlign: TextAlign.center),
              const SizedBox(height: 18),
              FilledButton.tonalIcon(
                onPressed: onRetry,
                icon: const Icon(Icons.refresh),
                label: const Text('重试'),
              ),
            ],
          ),
        ),
      );
    }
    if (empty) {
      return Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(emptyIcon, size: 52),
            const SizedBox(height: 12),
            Text(emptyText, style: Theme.of(context).textTheme.titleMedium),
          ],
        ),
      );
    }
    return AbsorbPointer(absorbing: loading, child: child);
  }
}

class _StatusChip extends StatelessWidget {
  const _StatusChip({required this.running, required this.label});
  final bool running;
  final String label;

  @override
  Widget build(BuildContext context) {
    final color = running
        ? const Color(0xff137333)
        : Theme.of(context).colorScheme.onSurfaceVariant;
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Icon(
          running ? Icons.play_circle_outline : Icons.pause_circle_outline,
          color: color,
          size: 18,
        ),
        const SizedBox(width: 6),
        Text(label),
      ],
    );
  }
}

class BockerCommand {
  BockerCommand();

  Process? _activeProcess;

  String get userHomeDirectory {
    final home = Platform.environment['HOME']?.trim();
    if (home != null && home.isNotEmpty) return home;
    return Directory.current.path;
  }

  Future<CommandResult> run(
    List<String> arguments, {
    String? workingDirectory,
    ValueChanged<String>? onOutput,
  }) async {
    return _runDirect(
      arguments,
      workingDirectory: workingDirectory,
      onOutput: onOutput,
    );
  }

  void cancelActive() {
    _activeProcess?.kill(ProcessSignal.sigterm);
  }

  Future<CommandResult> openShell(String containerName) async {
    try {
      final shellArguments = [_binary, 'container', 'shell', containerName];
      final title = 'Bocker: $containerName';
      // Each terminal takes slightly different option spellings:
      //   ptyxis:           -T <title> -- <command...>
      //   gnome-terminal:   --title <title> -- <command...>
      //   x-terminal-*:     -T <title> -e <command...>
      final ptyxis = _findExecutable('ptyxis');
      if (ptyxis != null) {
        await Process.start(ptyxis, [
          '-T',
          title,
          '--',
          ...shellArguments,
        ], mode: ProcessStartMode.detached);
        return const CommandResult(true, '', 0);
      }
      final gnomeTerminal = _findExecutable('gnome-terminal');
      if (gnomeTerminal != null) {
        await Process.start(gnomeTerminal, [
          '--title',
          title,
          '--',
          ...shellArguments,
        ], mode: ProcessStartMode.detached);
        return const CommandResult(true, '', 0);
      }
      await Process.start('x-terminal-emulator', [
        '-T',
        title,
        '-e',
        ...shellArguments,
      ], mode: ProcessStartMode.detached);
      return const CommandResult(true, '', 0);
    } on ProcessException catch (error) {
      return CommandResult(false, error.message, -1);
    }
  }

  String? _findExecutable(String name) {
    final path = Platform.environment['PATH'];
    if (path == null || path.isEmpty) return null;
    for (final directory in path.split(':')) {
      if (directory.isEmpty) continue;
      final candidate = File('$directory/$name');
      if (candidate.existsSync()) return candidate.path;
    }
    return null;
  }

  Future<CommandResult> _runDirect(
    List<String> arguments, {
    String? workingDirectory,
    ValueChanged<String>? onOutput,
  }) async {
    Process? process;
    try {
      process = await Process.start(
        _binary,
        arguments,
        runInShell: false,
        workingDirectory: workingDirectory,
      );
      _activeProcess = process;
      final output = StringBuffer();
      const maxOutput = 1024 * 1024;
      void appendOutput(String chunk) {
        output.write(chunk);
        if (output.length > maxOutput) {
          final text = output.toString();
          output
            ..clear()
            ..write('[较早输出已截断]\n')
            ..write(text.substring(text.length - maxOutput));
        }
        onOutput?.call(output.toString());
      }

      final stdoutDone = process.stdout
          .transform(utf8.decoder)
          .forEach(appendOutput);
      final stderrDone = process.stderr
          .transform(utf8.decoder)
          .forEach(appendOutput);
      final exitCode = await process.exitCode;
      await Future.wait([stdoutDone, stderrDone]);
      return CommandResult(exitCode == 0, output.toString().trim(), exitCode);
    } on ProcessException catch (error) {
      return CommandResult(false, '无法启动 $_binary: ${error.message}', -1);
    } finally {
      if (identical(_activeProcess, process)) _activeProcess = null;
    }
  }

  String get _binary {
    final configured = Platform.environment['BOCKER_BINARY']?.trim();
    if (configured != null && configured.isNotEmpty) {
      return configured;
    }
    return '${File(Platform.resolvedExecutable).parent.path}/bocker';
  }
}

class CommandResult {
  const CommandResult(this.ok, this.output, this.exitCode);
  final bool ok;
  final String output;
  final int exitCode;
  String get summary {
    final condensed = output.replaceAll(RegExp(r'\s+'), ' ').trim();
    if (condensed.length <= 160) return condensed;
    return '${condensed.substring(0, 160)}...';
  }
}

class ContainerInfo {
  const ContainerInfo({
    required this.name,
    required this.status,
    required this.network,
    required this.ipv4,
    required this.ipv6,
    required this.domain,
    required this.autostart,
    required this.ports,
  });
  final String name;
  final String status;
  final String network;
  final String ipv4;
  final String ipv6;
  final String domain;
  final String autostart;
  final String ports;
  bool get isRunning => status.toLowerCase() == 'running';
  String get portsDisplay {
    if (ports.isEmpty) return '-';
    return ports.split(', ').join('\n');
  }
}

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

String jsonText(Map item, String key) => item[key]?.toString() ?? '';

bool isValidBockerName(String name) {
  if (name.isEmpty || name.length > 63 || RegExp(r'^\d+$').hasMatch(name)) {
    return false;
  }
  return RegExp(
    r'^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$',
  ).hasMatch(name);
}

bool isValidDomain(String domain) {
  if (domain.isEmpty || domain.length > 253 || domain.endsWith('.')) {
    return false;
  }
  return domain
      .split('.')
      .every(
        (label) =>
            label.isNotEmpty &&
            label.length <= 63 &&
            RegExp(
              r'^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?$',
            ).hasMatch(label),
      );
}

bool isValidPortMapping(String spec) {
  final match = RegExp(
    r'^(\d+)(?::(\d+))?(?:/(tcp|udp))?$',
  ).firstMatch(spec.trim().toLowerCase());
  if (match == null) return false;
  final host = int.tryParse(match.group(1)!);
  final container = int.tryParse(match.group(2) ?? match.group(1)!);
  return host != null &&
      container != null &&
      host >= 1 &&
      host <= 65535 &&
      container >= 1 &&
      container <= 65535;
}

List<String> removablePortSpecs(String summary) {
  final result = <String>[];
  final pattern = RegExp(r'(?:^|,\s*)(\d+)(?:->\d+)?/(tcp|udp)');
  for (final match in pattern.allMatches(summary.toLowerCase())) {
    final spec = '${match.group(1)}/${match.group(2)}';
    if (!result.contains(spec)) result.add(spec);
  }
  return result;
}

/// Splits a command line into argv entries the way a POSIX shell would,
/// honoring single/double quotes and backslash escapes, without performing
/// variable expansion. Used to pass the interactive exec input through the
/// argv-based `container exec` API instead of a shell.
List<String> splitShellWords(String input) {
  final words = <String>[];
  final buffer = StringBuffer();
  var quote = '';
  var escaped = false;
  var hasToken = false;
  for (final rune in input.runes) {
    final ch = String.fromCharCode(rune);
    if (escaped) {
      buffer.write(ch);
      escaped = false;
      hasToken = true;
      continue;
    }
    if (quote.isNotEmpty) {
      if (ch == quote) {
        quote = '';
      } else if (ch == '\\' && quote == '"') {
        escaped = true;
      } else {
        buffer.write(ch);
      }
      hasToken = true;
      continue;
    }
    if (ch == '\\') {
      escaped = true;
      hasToken = true;
      continue;
    }
    if (ch == "'" || ch == '"') {
      quote = ch;
      hasToken = true;
      continue;
    }
    if (ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r') {
      if (hasToken) {
        words.add(buffer.toString());
        buffer.clear();
        hasToken = false;
      }
      continue;
    }
    buffer.write(ch);
    hasToken = true;
  }
  if (escaped) buffer.write('\\');
  // Tolerate unbalanced quotes by dropping the dangling quote character.
  if (hasToken || buffer.isNotEmpty) words.add(buffer.toString());
  return words;
}

class _InstallValues {
  const _InstallValues(this.image, this.name, this.network, this.permission);
  final String image;
  final String name;
  final String network;
  final String permission;
}

class _BuildValues {
  const _BuildValues(this.path, this.name, this.network);
  final String path;
  final String name;
  final String network;
}

class _RunImageValues {
  const _RunImageValues(this.name, this.network, this.permission);
  final String name;
  final String network;
  final String permission;
}

List<String> templateInstallArguments({
  required String image,
  required String name,
  required String network,
  required String permission,
}) {
  return [
    'template',
    'install',
    image,
    '--network',
    network,
    '--permission',
    permission,
    if (name.isNotEmpty) ...['--name', name],
  ];
}

List<String> imageRunArguments({
  required String image,
  required String name,
  required String network,
  required String permission,
}) {
  return [
    'image',
    'run',
    image,
    '--name',
    name,
    '--permission',
    permission,
    if (network != 'default') ...['--network', network],
  ];
}

class _ImportValues {
  const _ImportValues(this.path, this.name, this.network, this.permission);
  final String path;
  final String name;
  final String network;
  final String permission;
}

class _SettingsValues {
  const _SettingsValues(
    this.domain,
    this.port,
    this.removePort,
    this.autostart,
    this.network,
  );
  final String domain;
  final String port;
  final String removePort;
  final bool autostart;
  final String network;
}
