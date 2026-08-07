import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';

void main() {
  runApp(const BockerGuiApp());
}

class BockerGuiApp extends StatelessWidget {
  const BockerGuiApp({super.key});

  @override
  Widget build(BuildContext context) {
    const seed = Color(0xff006c51);
    return MaterialApp(
      title: 'Bocker',
      debugShowCheckedModeBanner: false,
      theme: ThemeData(
        colorScheme: ColorScheme.fromSeed(seedColor: seed),
        useMaterial3: true,
        scaffoldBackgroundColor: const Color(0xfff7faf8),
        appBarTheme: const AppBarTheme(centerTitle: false),
        inputDecorationTheme: const InputDecorationTheme(
          border: OutlineInputBorder(),
          isDense: true,
        ),
      ),
      darkTheme: ThemeData(
        colorScheme: ColorScheme.fromSeed(
          seedColor: seed,
          brightness: Brightness.dark,
        ),
        useMaterial3: true,
      ),
      themeMode: ThemeMode.system,
      home: const BockerHome(),
    );
  }
}

enum _Section { containers, images }

class BockerHome extends StatefulWidget {
  const BockerHome({super.key});

  @override
  State<BockerHome> createState() => _BockerHomeState();
}

class _BockerHomeState extends State<BockerHome> {
  final _bocker = BockerCommand();
  _Section _section = _Section.containers;
  List<ContainerInfo> _containers = const [];
  List<ImageInfo> _images = const [];
  bool _loading = false;
  String? _loadError;
  String _lastOutput = '';

  @override
  void initState() {
    super.initState();
    _refresh();
  }

  Future<void> _refresh() async {
    setState(() {
      _loading = true;
      _loadError = null;
    });
    final result = await _bocker.run(
      _section == _Section.containers
          ? ['container', 'list', '--json']
          : ['image', 'list', '--json'],
    );
    if (!mounted) return;
    setState(() {
      _loading = false;
      _lastOutput = result.output;
      if (result.ok) {
        _loadError = null;
        if (_section == _Section.containers) {
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
  }) async {
    setState(() => _loading = true);
    final result = await _bocker.run(arguments);
    if (!mounted) return result;
    setState(() {
      _loading = false;
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
    setState(() => _loading = true);
    final result = await _bocker.run(['template', 'list', '--json']);
    if (!mounted) return null;
    setState(() {
      _loading = false;
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
          content: SizedBox(
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
                Row(
                  children: [
                    Expanded(
                      child: DropdownButtonFormField<String>(
                        initialValue: network,
                        decoration: const InputDecoration(labelText: '网络模式'),
                        items: const [
                          DropdownMenuItem(value: 'nat', child: Text('NAT')),
                          DropdownMenuItem(
                            value: 'bridge',
                            child: Text('Bridge'),
                          ),
                        ],
                        onChanged: (value) =>
                            setDialogState(() => network = value!),
                      ),
                    ),
                    const SizedBox(width: 16),
                    Expanded(
                      child: DropdownButtonFormField<String>(
                        initialValue: permission,
                        decoration: const InputDecoration(labelText: '容器权限'),
                        items: const [
                          DropdownMenuItem(value: 'normal', child: Text('普通')),
                          DropdownMenuItem(value: 'super', child: Text('超级')),
                        ],
                        onChanged: (value) =>
                            setDialogState(() => permission = value!),
                      ),
                    ),
                  ],
                ),
              ],
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
    if (values == null) return;
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
          content: SizedBox(
            width: 440,
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                TextField(
                  controller: path,
                  decoration: const InputDecoration(labelText: 'Incusfile 路径'),
                ),
                const SizedBox(height: 16),
                TextField(
                  controller: name,
                  decoration: const InputDecoration(labelText: '镜像别名（可选）'),
                ),
                const SizedBox(height: 16),
                DropdownButtonFormField<String>(
                  initialValue: network,
                  decoration: const InputDecoration(labelText: '构建网络'),
                  items: const [
                    DropdownMenuItem(value: 'nat', child: Text('NAT')),
                    DropdownMenuItem(value: 'bridge', child: Text('Bridge')),
                  ],
                  onChanged: (value) => setDialogState(() => network = value!),
                ),
              ],
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
    if (values == null || values.path.isEmpty) return;
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
          content: SizedBox(
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
                Row(
                  children: [
                    Expanded(
                      child: DropdownButtonFormField<String>(
                        initialValue: network,
                        decoration: const InputDecoration(labelText: '网络模式'),
                        items: const [
                          DropdownMenuItem(
                            value: 'default',
                            child: Text('使用镜像设置'),
                          ),
                          DropdownMenuItem(value: 'nat', child: Text('NAT')),
                          DropdownMenuItem(
                            value: 'bridge',
                            child: Text('Bridge'),
                          ),
                        ],
                        onChanged: (value) =>
                            setDialogState(() => network = value!),
                      ),
                    ),
                    const SizedBox(width: 16),
                    Expanded(
                      child: DropdownButtonFormField<String>(
                        initialValue: permission,
                        decoration: const InputDecoration(labelText: '容器权限'),
                        items: const [
                          DropdownMenuItem(value: 'normal', child: Text('普通')),
                          DropdownMenuItem(value: 'super', child: Text('超级')),
                        ],
                        onChanged: (value) =>
                            setDialogState(() => permission = value!),
                      ),
                    ),
                  ],
                ),
              ],
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
    if (values == null) return;
    if (values.name.isEmpty) {
      if (!mounted) return;
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('容器名称不能为空。')));
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
    final values = await showDialog<_ImportValues>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('导入备份'),
        content: SizedBox(
          width: 440,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              TextField(
                controller: path,
                autofocus: true,
                decoration: const InputDecoration(labelText: '备份文件路径'),
              ),
              const SizedBox(height: 16),
              TextField(
                controller: name,
                decoration: const InputDecoration(labelText: '容器名称（可选）'),
              ),
            ],
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
              _ImportValues(path.text.trim(), name.text.trim()),
            ),
            child: const Text('导入'),
          ),
        ],
      ),
    );
    if (values == null || values.path.isEmpty) return;
    await _run('导入备份', [
      'container',
      'import',
      values.path,
      if (values.name.isNotEmpty) values.name,
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
    if (value == null || value.isEmpty) return;
    final result = await _run('执行命令', [
      'container',
      'exec',
      container.name,
      value,
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
    final domain = TextEditingController();
    final port = TextEditingController();
    final removePort = TextEditingController();
    var autostart = container.autostart == 'on';
    var network = container.network;
    final values = await showDialog<_SettingsValues>(
      context: context,
      builder: (context) => StatefulBuilder(
        builder: (context, setDialogState) => AlertDialog(
          title: Text('${container.name} 设置'),
          content: SizedBox(
            width: 460,
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                TextField(
                  controller: domain,
                  decoration: const InputDecoration(
                    labelText: '域名映射',
                    hintText: '例如 app.test；输入 - 取消映射',
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
                const SizedBox(height: 16),
                TextField(
                  controller: removePort,
                  decoration: const InputDecoration(
                    labelText: '删除端口映射',
                    hintText: '例如 8080/tcp',
                  ),
                ),
                const SizedBox(height: 12),
                SwitchListTile(
                  contentPadding: EdgeInsets.zero,
                  title: const Text('开机自启动'),
                  value: autostart,
                  onChanged: (value) => setDialogState(() => autostart = value),
                ),
                DropdownButtonFormField<String>(
                  initialValue: network,
                  decoration: InputDecoration(
                    labelText: '网络模式',
                    helperText: container.isRunning ? '运行中的容器不能切换网络模式' : null,
                  ),
                  items: const [
                    DropdownMenuItem(value: 'nat', child: Text('NAT')),
                    DropdownMenuItem(value: 'bridge', child: Text('Bridge')),
                  ],
                  onChanged: container.isRunning
                      ? null
                      : (value) => setDialogState(() => network = value!),
                ),
                if (container.ports != '-' && container.ports.isNotEmpty) ...[
                  const SizedBox(height: 14),
                  Text(
                    '当前端口: ${container.ports}',
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                ],
              ],
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
                  removePort.text.trim(),
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
    if (values == null) return;
    var ok = true;
    if (values.domain.isNotEmpty) {
      ok = (await _run('保存域名', [
        'container',
        'set',
        container.name,
        'domain',
        values.domain == '-' ? '--unset' : values.domain,
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
              FilledButton.tonal(
                onPressed: () => Navigator.pop(context, true),
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
            onInstall: _installDialog,
            onBuild: _buildDialog,
            onImport: _importDialog,
            onStart: (item) => _run('启动容器', ['container', 'start', item.name]),
            onStop: (item) => _run('停止容器', ['container', 'stop', item.name]),
            onRestart: (item) =>
                _run('重启容器', ['container', 'restart', item.name]),
            onExport: (item) =>
                _run('导出容器', ['container', 'export', item.name]),
            onOpenShell: _openContainerShell,
            onExec: _execDialog,
            onSettings: _settingsDialog,
            onDelete: _deleteContainer,
          )
        : _ImagesView(
            images: _images,
            loading: _loading,
            error: _loadError,
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
          : Drawer(
              child: SafeArea(
                child: Column(
                  children: [
                    const ListTile(
                      leading: Icon(Icons.developer_board),
                      title: Text('Bocker'),
                    ),
                    const Divider(),
                    ListTile(
                      leading: const Icon(Icons.inventory_2_outlined),
                      selected: _section == _Section.containers,
                      title: const Text('容器'),
                      onTap: () {
                        Navigator.pop(context);
                        _changeSection(_Section.containers);
                      },
                    ),
                    ListTile(
                      leading: const Icon(Icons.layers_outlined),
                      selected: _section == _Section.images,
                      title: const Text('镜像'),
                      onTap: () {
                        Navigator.pop(context);
                        _changeSection(_Section.images);
                      },
                    ),
                  ],
                ),
              ),
            ),
      body: Row(
        children: [
          if (wide) rail,
          if (wide) const VerticalDivider(width: 1),
          Expanded(child: body),
        ],
      ),
    );
  }
}

class _ContainersView extends StatelessWidget {
  const _ContainersView({
    required this.containers,
    required this.loading,
    required this.error,
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
        empty: containers.isEmpty,
        emptyIcon: Icons.inventory_2_outlined,
        emptyText: '还没有容器',
        child: LayoutBuilder(
          builder: (context, constraints) => SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            child: ConstrainedBox(
              constraints: BoxConstraints(minWidth: constraints.maxWidth),
              child: DataTable(
                columnSpacing: 28,
                columns: const [
                  DataColumn(label: Text('名称')),
                  DataColumn(label: Text('状态')),
                  DataColumn(label: Text('网络')),
                  DataColumn(label: Text('IPv4')),
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
                          DataCell(Text(item.ipv4)),
                          DataCell(
                            SizedBox(
                              width: 150,
                              child: Text(
                                item.ports,
                                overflow: TextOverflow.ellipsis,
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
                                    icon: const Icon(Icons.play_circle_outline),
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
          ),
        ),
      ),
    );
  }
}

enum _ContainerAction { exec, settings, export, delete }

class _ImagesView extends StatelessWidget {
  const _ImagesView({
    required this.images,
    required this.loading,
    required this.error,
    required this.onBuild,
    required this.onRun,
    required this.onDelete,
  });

  final List<ImageInfo> images;
  final bool loading;
  final String? error;
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
    return Padding(
      padding: const EdgeInsets.all(28),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Wrap(
            spacing: 12,
            runSpacing: 12,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(title, style: Theme.of(context).textTheme.headlineSmall),
                  Text(subtitle, style: Theme.of(context).textTheme.bodyMedium),
                ],
              ),
              const SizedBox(width: 12),
              primary,
              ...actions,
            ],
          ),
          const SizedBox(height: 24),
          Expanded(child: child),
        ],
      ),
    );
  }
}

class _ContentState extends StatelessWidget {
  const _ContentState({
    required this.loading,
    required this.error,
    required this.empty,
    required this.emptyIcon,
    required this.emptyText,
    required this.child,
  });

  final bool loading;
  final String? error;
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
    return Stack(
      children: [
        Positioned.fill(child: child),
        if (loading)
          const Positioned(
            top: 0,
            left: 0,
            right: 0,
            child: LinearProgressIndicator(),
          ),
      ],
    );
  }
}

class _StatusChip extends StatelessWidget {
  const _StatusChip({required this.running, required this.label});
  final bool running;
  final String label;

  @override
  Widget build(BuildContext context) {
    final color = running
        ? Theme.of(context).colorScheme.primary
        : Theme.of(context).colorScheme.outline;
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

  Future<CommandResult> run(List<String> arguments) async {
    return _runDirect(arguments);
  }

  Future<CommandResult> openShell(String containerName) async {
    try {
      final shellArguments = [_binary, 'container', 'shell', containerName];
      final nativeTerminal =
          _findExecutable('ptyxis') ?? _findExecutable('gnome-terminal');
      if (nativeTerminal != null) {
        await Process.start(nativeTerminal, [
          '-T',
          'Bocker: $containerName',
          '--',
          ...shellArguments,
        ], mode: ProcessStartMode.detached);
      } else {
        await Process.start('x-terminal-emulator', [
          '-T',
          'Bocker: $containerName',
          '-e',
          ...shellArguments,
        ], mode: ProcessStartMode.detached);
      }
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

  Future<CommandResult> _runDirect(List<String> arguments) async {
    try {
      final result = await Process.run(_binary, arguments, runInShell: false);
      return CommandResult(
        result.exitCode == 0,
        '${result.stdout}${result.stderr}'.trim(),
        result.exitCode,
      );
    } on ProcessException catch (error) {
      return CommandResult(false, '无法启动 $_binary: ${error.message}', -1);
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
    required this.autostart,
    required this.ports,
  });
  final String name;
  final String status;
  final String network;
  final String ipv4;
  final String ipv6;
  final String autostart;
  final String ports;
  bool get isRunning => status.toLowerCase() == 'running';
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
    return decoded.whereType<Map<String, dynamic>>().map((item) {
      return ContainerInfo(
        name: item['name'] as String? ?? '',
        status: item['status'] as String? ?? '',
        network: item['network'] as String? ?? '',
        ipv4: item['ipv4'] as String? ?? '',
        ipv6: item['ipv6'] as String? ?? '',
        autostart: item['autostart'] as String? ?? '',
        ports: item['ports'] as String? ?? '',
      );
    }).toList();
  } on FormatException {
    return const [];
  }
}

List<ImageInfo> parseImages(String output) {
  try {
    final decoded = jsonDecode(output);
    if (decoded is! List) return const [];
    return decoded.whereType<Map<String, dynamic>>().map((item) {
      return ImageInfo(
        name: item['name'] as String? ?? '',
        size: item['size'] as String? ?? '',
        created: item['created'] as String? ?? '',
        fingerprint: item['fingerprint'] as String? ?? '',
      );
    }).toList();
  } on FormatException {
    return const [];
  }
}

List<ImageTemplate> parseImageTemplates(String output) {
  try {
    final decoded = jsonDecode(output);
    if (decoded is! List) return const [];
    return decoded.whereType<Map<String, dynamic>>().map((item) {
      return ImageTemplate(
        distro: item['distro'] as String? ?? '',
        release: item['release'] as String? ?? '',
        image: item['image'] as String? ?? '',
      );
    }).toList();
  } on FormatException {
    return const [];
  }
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
  const _ImportValues(this.path, this.name);
  final String path;
  final String name;
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
