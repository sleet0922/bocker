import 'package:flutter/material.dart';

import 'models.dart';
import 'ssh_service.dart';
import 'terminal_page.dart';
import 'widgets.dart';

/// 容器详情页：信息展示 + 生命周期操作 + 全部运行时设置
/// （网络模式 / 域名映射 / 自启动 / 端口映射 / 挂载）。
class ContainerDetailPage extends StatefulWidget {
  const ContainerDetailPage({
    super.key,
    required this.service,
    required this.containerName,
  });

  final SshService service;
  final String containerName;

  @override
  State<ContainerDetailPage> createState() => _ContainerDetailPageState();
}

class _ContainerDetailPageState extends State<ContainerDetailPage> {
  ContainerInfo? _container;
  List<MountInfo> _mounts = const [];
  bool _loading = false;
  String? _error;

  SshService get _service => widget.service;

  String get _name => widget.containerName;

  @override
  void initState() {
    super.initState();
    refresh();
  }

  Future<void> refresh() async {
    if (_loading) return;
    setState(() {
      _loading = true;
      _error = null;
    });
    final result = await _service.run(
      const ['container', 'list', '--json'],
      timeout: const Duration(seconds: 60),
    );
    if (!mounted) return;
    if (!result.ok) {
      setState(() {
        _loading = false;
        _error = result.summary;
      });
      return;
    }
    final mine = parseContainers(result.output)
        .where((c) => c.name == _name)
        .toList();
    if (mine.isEmpty) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = '容器 $_name 不存在（可能已被删除）';
      });
      return;
    }
    // 挂载列表独立加载，失败不阻塞主信息。
    final mountResult = await _service.run(
      ['container', 'set', _name, 'mount', 'list', '--json'],
      timeout: const Duration(seconds: 30),
    );
    if (!mounted) return;
    setState(() {
      _loading = false;
      _container = mine.first;
      _mounts = mountResult.ok ? parseMounts(mountResult.output) : const [];
    });
  }

  /// 执行 `bocker container set ...` 变更并刷新。
  Future<void> _applySetting(
    String label,
    List<String> args, {
    Duration timeout = const Duration(seconds: 120),
  }) async {
    final result = await runWithProgressDialog(
      context,
      _service,
      label,
      ['container', 'set', _name, ...args],
      timeout: timeout,
    );
    if (!mounted) return;
    showResultSnackBar(context, result, successLabel: label);
    if (result.ok) await refresh();
  }

  Future<void> _runLifecycle(
    String label,
    List<String> args, {
    Duration timeout = const Duration(seconds: 180),
  }) async {
    final result = await runWithProgressDialog(
      context,
      _service,
      label,
      args,
      timeout: timeout,
    );
    if (!mounted) return;
    showResultSnackBar(context, result, successLabel: label);
    if (result.ok) await refresh();
  }

  // ---------------------------------------------------------------- 网络

  Future<void> _switchNetwork(String mode) async {
    final container = _container;
    if (container == null || container.network == mode) return;
    if (container.isRunning) {
      _hint('容器正在运行，请先停止容器再切换网络模式');
      return;
    }
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('切换网络模式'),
        content: Text('将容器 $_name 的网络从 ${container.network} 切换为 $mode？'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(dialogContext).pop(true),
            child: const Text('切换'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    await _applySetting('切换网络模式', ['network', mode]);
  }

  // ---------------------------------------------------------------- 域名

  Future<void> _editDomain() async {
    final container = _container;
    if (container == null) return;
    final controller = TextEditingController(text: container.domain);
    final action = await showDialog<({bool save, String domain})>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('域名映射'),
        content: TextField(
          controller: controller,
          autofocus: true,
          decoration: const InputDecoration(
            labelText: '域名（如 alpine.test）',
            hintText: '留空表示取消映射',
          ),
        ),
        actions: [
          if (container.domain.isNotEmpty)
            TextButton(
              onPressed: () => Navigator.of(dialogContext).pop(
                (save: true, domain: ''),
              ),
              child: const Text('取消映射'),
            ),
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(),
            child: const Text('关闭'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(dialogContext).pop(
              (save: true, domain: controller.text.trim()),
            ),
            child: const Text('保存'),
          ),
        ],
      ),
    );
    controller.dispose();
    if (action == null) return;
    if (!action.save) return;
    if (action.domain.isEmpty) {
      await _applySetting('取消域名映射', ['domain', '--unset']);
    } else {
      await _applySetting('设置域名映射', ['domain', action.domain]);
    }
  }

  // ---------------------------------------------------------------- 自启动

  Future<void> _toggleAutostart(bool? value) async {
    if (value == null) return;
    await _applySetting(
      value ? '开启自启动' : '关闭自启动',
      ['autostart', value ? 'on' : 'off'],
    );
  }

  // ---------------------------------------------------------------- 端口

  Future<void> _addPort() async {
    final spec = await _promptText(
      title: '添加端口映射',
      label: '规格：宿主端口[:容器端口][/协议]',
      hint: '如 8080、8080:80、53/udp、8080:80/tcp',
    );
    if (spec == null || spec.isEmpty) return;
    await _applySetting('添加端口映射', ['port', spec]);
  }

  Future<void> _removePort() async {
    final container = _container;
    if (container == null) return;
    final entries = container.portList;
    if (entries.isEmpty) {
      _hint('当前没有端口映射');
      return;
    }
    // 条目形如 "8080/tcp(v4)" / "8080->80/tcp"，移除需要 宿主端口/协议。
    final removable = entries
        .map((entry) => entry.replaceAll(RegExp(r'\(.*\)$'), ''))
        .toList();
    final index = await _pickFromList('选择要移除的端口映射', removable);
    if (index == null) return;
    // "8080->80/tcp" -> "8080/tcp"；"8080/tcp" 保持不变。
    final spec = removable[index].split('->').first.trim();
    await _applySetting('移除端口映射', ['port', 'rm', spec]);
  }

  // ---------------------------------------------------------------- 挂载

  Future<void> _addMount() async {
    final source = await _promptText(
      title: '添加挂载',
      label: '宿主机绝对路径',
      hint: '如 /data/www',
    );
    if (source == null || source.isEmpty) return;
    final target = await _promptText(
      title: '添加挂载',
      label: '容器内绝对路径',
      hint: '如 /var/www',
    );
    if (target == null || target.isEmpty) return;
    final mode = await _pickFromList('挂载模式', const ['rw（读写）', 'ro（只读）']);
    if (mode == null) return;
    await _applySetting('添加挂载', [
      'mount',
      'add',
      source,
      target,
      mode == 0 ? 'rw' : 'ro',
    ]);
  }

  Future<void> _removeMount() async {
    if (_mounts.isEmpty) {
      _hint('当前没有挂载');
      return;
    }
    final index = await _pickFromList(
      '选择要删除的挂载',
      _mounts.map((m) => m.displayLabel).toList(),
    );
    if (index == null) return;
    await _applySetting('删除挂载', ['mount', 'rm', _mounts[index].name]);
  }

  // ---------------------------------------------------------------- 生命周期

  void _confirmRemoveContainer() {
    showDialog<void>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: Text('删除容器 $_name'),
        content: const Text('删除后容器及其数据将无法恢复，确定删除吗？'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(),
            child: const Text('取消'),
          ),
          FilledButton(
            style: FilledButton.styleFrom(
              backgroundColor: Theme.of(dialogContext).colorScheme.error,
            ),
            onPressed: () {
              Navigator.of(dialogContext).pop();
              _runLifecycle(
                '删除容器',
                ['container', 'remove', _name],
                timeout: const Duration(seconds: 300),
              );
            },
            child: const Text('删除'),
          ),
        ],
      ),
    );
  }

  void _openTerminal() {
    Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => TerminalPage(service: _service, containerName: _name),
      ),
    );
  }

  // ---------------------------------------------------------------- 辅助

  void _hint(String message) {
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text(message), behavior: SnackBarBehavior.floating),
    );
  }

  Future<String?> _promptText({
    required String title,
    required String label,
    String? hint,
  }) {
    final controller = TextEditingController();
    return showDialog<String>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: Text(title),
        content: TextField(
          controller: controller,
          autofocus: true,
          decoration: InputDecoration(labelText: label, hintText: hint),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () =>
                Navigator.of(dialogContext).pop(controller.text.trim()),
            child: const Text('确定'),
          ),
        ],
      ),
    ).whenComplete(controller.dispose);
  }

  Future<int?> _pickFromList(String title, List<String> options) {
    return showDialog<int>(
      context: context,
      builder: (dialogContext) => SimpleDialog(
        title: Text(title),
        children: [
          for (var i = 0; i < options.length; i++)
            SimpleDialogOption(
              onPressed: () => Navigator.of(dialogContext).pop(i),
              child: Text(options[i]),
            ),
        ],
      ),
    );
  }

  // ---------------------------------------------------------------- UI

  @override
  Widget build(BuildContext context) {
    final container = _container;
    return Scaffold(
      appBar: AppBar(
        title: Text(_name),
        actions: [
          IconButton(
            tooltip: '刷新',
            icon: const Icon(Icons.refresh),
            onPressed: refresh,
          ),
        ],
      ),
      body: _error != null
          ? _ErrorView(message: _error!, onRetry: refresh)
          : container == null
          ? const Center(child: CircularProgressIndicator())
          : RefreshIndicator(
              onRefresh: refresh,
              child: ListView(
                physics: const AlwaysScrollableScrollPhysics(),
                padding: const EdgeInsets.fromLTRB(16, 8, 16, 24),
                children: [
                  _overviewCard(container),
                  const SizedBox(height: 12),
                  _actionsCard(container),
                  const SizedBox(height: 12),
                  _settingsCard(container),
                ],
              ),
            ),
    );
  }

  Widget _overviewCard(ContainerInfo container) {
    final colors = Theme.of(context).colorScheme;
    Widget row(String label, String value) => Padding(
      padding: const EdgeInsets.symmetric(vertical: 4),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 72,
            child: Text(
              label,
              style: TextStyle(color: colors.onSurfaceVariant),
            ),
          ),
          Expanded(
            child: Text(value.isEmpty || value == 'N/A' ? '-' : value),
          ),
        ],
      ),
    );
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              children: [
                Expanded(
                  child: Text(
                    container.name,
                    style: Theme.of(context).textTheme.titleLarge,
                  ),
                ),
                _StatusChip(status: container.status),
              ],
            ),
            const Divider(height: 24),
            row('IPv4', container.ipv4),
            row('IPv6', container.ipv6),
            row('内存', container.memory),
            row('网络', container.network),
            row('域名', container.domain),
            row('端口', container.ports),
          ],
        ),
      ),
    );
  }

  Widget _actionsCard(ContainerInfo container) {
    final colors = Theme.of(context).colorScheme;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text('操作', style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 12),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                if (!container.isRunning)
                  FilledButton.icon(
                    onPressed: () => _runLifecycle(
                      '启动容器',
                      ['container', 'start', container.name],
                    ),
                    icon: const Icon(Icons.play_arrow),
                    label: const Text('启动'),
                  ),
                if (container.isRunning)
                  FilledButton.icon(
                    onPressed: () => _runLifecycle(
                      '停止容器',
                      ['container', 'stop', container.name],
                    ),
                    icon: const Icon(Icons.stop),
                    label: const Text('停止'),
                  ),
                OutlinedButton.icon(
                  onPressed: () => _runLifecycle(
                    '重启容器',
                    ['container', 'restart', container.name],
                  ),
                  icon: const Icon(Icons.refresh),
                  label: const Text('重启'),
                ),
                OutlinedButton.icon(
                  onPressed: container.isRunning ? _openTerminal : null,
                  icon: const Icon(Icons.terminal),
                  label: const Text('终端'),
                ),
                OutlinedButton.icon(
                  style: OutlinedButton.styleFrom(foregroundColor: colors.error),
                  onPressed: _confirmRemoveContainer,
                  icon: const Icon(Icons.delete_outline),
                  label: const Text('删除'),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }

  Widget _settingsCard(ContainerInfo container) {
    final colors = Theme.of(context).colorScheme;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text('设置', style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 4),

            // 自启动开关
            SwitchListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('开机自启动'),
              subtitle: Text(
                container.autostart == 'on' ? '已开启' : '已关闭',
                style: TextStyle(color: colors.onSurfaceVariant),
              ),
              value: container.autostart == 'on',
              onChanged: _toggleAutostart,
            ),

            // 网络模式
            ListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('网络模式'),
              subtitle: Text(
                container.isRunning ? '运行中的容器需先停止才能切换' : 'NAT 共享宿主网络 / Bridge 独立网段',
                style: TextStyle(color: colors.onSurfaceVariant),
              ),
              trailing: SegmentedButton<String>(
                showSelectedIcon: false,
                segments: const [
                  ButtonSegment(value: 'nat', label: Text('NAT')),
                  ButtonSegment(value: 'bridge', label: Text('Bridge')),
                ],
                selected: {
                  container.network.toLowerCase() == 'bridge'
                      ? 'bridge'
                      : 'nat',
                },
                onSelectionChanged: container.isRunning
                    ? null
                    : (value) => _switchNetwork(value.first),
              ),
            ),

            // 域名映射
            ListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('域名映射'),
              subtitle: Text(
                container.domain.isEmpty || container.domain == 'N/A'
                    ? '未设置'
                    : container.domain,
                style: TextStyle(color: colors.onSurfaceVariant),
              ),
              trailing: const Icon(Icons.chevron_right),
              onTap: _editDomain,
            ),

            const Divider(),

            // 端口映射
            ListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('端口映射'),
              subtitle: Text(
                container.portList.isEmpty
                    ? '未设置'
                    : container.portList.join('\n'),
                style: TextStyle(
                  color: colors.onSurfaceVariant,
                  fontFamily: 'monospace',
                ),
              ),
              trailing: Wrap(
                spacing: 4,
                children: [
                  IconButton(
                    tooltip: '添加端口映射',
                    icon: const Icon(Icons.add_circle_outline),
                    onPressed: _addPort,
                  ),
                  IconButton(
                    tooltip: '移除端口映射',
                    icon: const Icon(Icons.remove_circle_outline),
                    onPressed: _removePort,
                  ),
                ],
              ),
            ),

            const Divider(),

            // 挂载
            ListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('文件挂载'),
              subtitle: _mounts.isEmpty
                  ? Text(
                      '未设置',
                      style: TextStyle(color: colors.onSurfaceVariant),
                    )
                  : Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        for (final mount in _mounts)
                          Text(
                            mount.displayLabel,
                            style: TextStyle(
                              color: colors.onSurfaceVariant,
                              fontFamily: 'monospace',
                              fontSize: 12,
                            ),
                          ),
                      ],
                    ),
              trailing: Wrap(
                spacing: 4,
                children: [
                  IconButton(
                    tooltip: '添加挂载',
                    icon: const Icon(Icons.add_circle_outline),
                    onPressed: _addMount,
                  ),
                  IconButton(
                    tooltip: '删除挂载',
                    icon: const Icon(Icons.remove_circle_outline),
                    onPressed: _removeMount,
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _ErrorView extends StatelessWidget {
  const _ErrorView({required this.message, required this.onRetry});

  final String message;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.error_outline, size: 48, color: colors.error),
            const SizedBox(height: 12),
            Text(message, textAlign: TextAlign.center),
            const SizedBox(height: 12),
            FilledButton(onPressed: onRetry, child: const Text('重试')),
          ],
        ),
      ),
    );
  }
}

class _StatusChip extends StatelessWidget {
  const _StatusChip({required this.status});

  final String status;

  Color _color(BuildContext context) {
    final value = status.toLowerCase();
    final colors = Theme.of(context).colorScheme;
    if (value == 'running') return Colors.green;
    if (value == 'stopped') return colors.outline;
    if (value == 'frozen') return Colors.lightBlue;
    if (value.contains('fail') || value.contains('error')) {
      return colors.error;
    }
    return colors.secondary;
  }

  @override
  Widget build(BuildContext context) {
    final color = _color(context);
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.15),
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(
        status,
        style: TextStyle(
          color: color,
          fontSize: 12,
          fontWeight: FontWeight.w600,
        ),
      ),
    );
  }
}
