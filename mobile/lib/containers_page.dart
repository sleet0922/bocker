import 'package:flutter/material.dart';

import 'models.dart';
import 'ssh_service.dart';
import 'widgets.dart';

class ContainersPage extends StatefulWidget {
  const ContainersPage({super.key, required this.service});

  final SshService service;

  @override
  State<ContainersPage> createState() => ContainersPageState();
}

class ContainersPageState extends State<ContainersPage> {
  List<ContainerInfo> _containers = const [];
  bool _loading = false;
  String? _error;

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
    final result = await widget.service.run(
      const ['container', 'list', '--json'],
      timeout: const Duration(seconds: 60),
    );
    if (!mounted) return;
    setState(() {
      _loading = false;
      if (result.ok) {
        _containers = parseContainers(result.output);
      } else {
        _error = result.summary;
      }
    });
  }

  Future<void> _runAction(
    String label,
    List<String> args, {
    Duration timeout = const Duration(seconds: 180),
  }) async {
    final result = await runWithProgressDialog(
      context,
      widget.service,
      label,
      args,
      timeout: timeout,
    );
    if (!mounted) return;
    showResultSnackBar(context, result, successLabel: label);
    if (result.ok) await refresh();
  }

  void _onMenuSelected(ContainerInfo container, String action) {
    switch (action) {
      case 'start':
        _runAction('启动容器', ['container', 'start', container.name]);
      case 'stop':
        _runAction('停止容器', ['container', 'stop', container.name]);
      case 'restart':
        _runAction('重启容器', ['container', 'restart', container.name]);
      case 'remove':
        _confirmRemove(container);
    }
  }

  void _confirmRemove(ContainerInfo container) {
    showDialog<void>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: Text('删除容器 ${container.name}'),
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
              _runAction(
                '删除容器',
                ['container', 'remove', container.name],
                timeout: const Duration(seconds: 300),
              );
            },
            child: const Text('删除'),
          ),
        ],
      ),
    );
  }

  void _showDetails(ContainerInfo container) {
    showModalBottomSheet<void>(
      context: context,
      showDragHandle: true,
      isScrollControlled: true,
      builder: (sheetContext) {
        final colors = Theme.of(sheetContext).colorScheme;
        Widget row(String label, String value) => Padding(
          padding: const EdgeInsets.symmetric(vertical: 4),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              SizedBox(
                width: 76,
                child: Text(
                  label,
                  style: TextStyle(color: colors.onSurfaceVariant),
                ),
              ),
              Expanded(child: Text(value.isEmpty ? '-' : value)),
            ],
          ),
        );
        return SafeArea(
          child: Padding(
            padding: const EdgeInsets.fromLTRB(20, 0, 20, 16),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Row(
                  children: [
                    Expanded(
                      child: Text(
                        container.name,
                        style: Theme.of(sheetContext).textTheme.titleLarge,
                      ),
                    ),
                    _StatusChip(status: container.status),
                  ],
                ),
                const SizedBox(height: 12),
                row('状态', container.status),
                row('IPv4', container.ipv4),
                row('IPv6', container.ipv6),
                row('网络', container.network),
                row('内存', container.memory),
                row('域名', container.domain),
                row('自启动', container.autostart),
                row('端口', container.ports.isEmpty ? '-' : container.ports.split(', ').join('\n')),
                const SizedBox(height: 16),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    if (!container.isRunning)
                      FilledButton.icon(
                        onPressed: () {
                          Navigator.of(sheetContext).pop();
                          _runAction('启动容器', [
                            'container',
                            'start',
                            container.name,
                          ]);
                        },
                        icon: const Icon(Icons.play_arrow),
                        label: const Text('启动'),
                      ),
                    if (container.isRunning)
                      FilledButton.icon(
                        onPressed: () {
                          Navigator.of(sheetContext).pop();
                          _runAction('停止容器', [
                            'container',
                            'stop',
                            container.name,
                          ]);
                        },
                        icon: const Icon(Icons.stop),
                        label: const Text('停止'),
                      ),
                    FilledButton.icon(
                      onPressed: () {
                        Navigator.of(sheetContext).pop();
                        _runAction('重启容器', [
                          'container',
                          'restart',
                          container.name,
                        ]);
                      },
                      icon: const Icon(Icons.refresh),
                      label: const Text('重启'),
                    ),
                    OutlinedButton.icon(
                      style: OutlinedButton.styleFrom(
                        foregroundColor: colors.error,
                      ),
                      onPressed: () {
                        Navigator.of(sheetContext).pop();
                        _confirmRemove(container);
                      },
                      icon: const Icon(Icons.delete_outline),
                      label: const Text('删除'),
                    ),
                  ],
                ),
              ],
            ),
          ),
        );
      },
    );
  }

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    if (_loading && _containers.isEmpty && _error == null) {
      return const Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            CircularProgressIndicator(),
            SizedBox(height: 12),
            Text('正在获取容器列表…'),
          ],
        ),
      );
    }
    return RefreshIndicator(
      onRefresh: refresh,
      child: ListView(
        physics: const AlwaysScrollableScrollPhysics(),
        padding: const EdgeInsets.fromLTRB(12, 8, 12, 16),
        children: [
          if (_error != null)
            Card(
              margin: const EdgeInsets.only(bottom: 8),
              color: colors.errorContainer,
              child: Padding(
                padding: const EdgeInsets.all(12),
                child: Row(
                  children: [
                    Icon(Icons.error_outline, color: colors.onErrorContainer),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Text(
                        _error!,
                        style: TextStyle(color: colors.onErrorContainer),
                      ),
                    ),
                    TextButton(onPressed: refresh, child: const Text('重试')),
                  ],
                ),
              ),
            ),
          if (_containers.isEmpty && _error == null)
            Padding(
              padding: const EdgeInsets.only(top: 80),
              child: Column(
                children: [
                  Icon(
                    Icons.inbox_outlined,
                    size: 56,
                    color: colors.onSurfaceVariant,
                  ),
                  const SizedBox(height: 12),
                  const Text('暂无容器'),
                  const SizedBox(height: 4),
                  Text(
                    '到“模板”页安装一个系统模板吧',
                    style: TextStyle(color: colors.onSurfaceVariant),
                  ),
                ],
              ),
            ),
          for (final container in _containers)
            Card(
              margin: const EdgeInsets.only(bottom: 8),
              child: ListTile(
                leading: Icon(
                  container.isRunning
                      ? Icons.play_circle_fill
                      : Icons.pause_circle_outlined,
                  color: container.isRunning ? Colors.green : colors.outline,
                ),
                title: Text(
                  container.name,
                  style: const TextStyle(fontWeight: FontWeight.w600),
                ),
                subtitle: Text(_subtitle(container)),
                trailing: PopupMenuButton<String>(
                  onSelected: (action) => _onMenuSelected(container, action),
                  itemBuilder: (_) => [
                    if (!container.isRunning)
                      const PopupMenuItem(value: 'start', child: Text('启动')),
                    if (container.isRunning)
                      const PopupMenuItem(value: 'stop', child: Text('停止')),
                    const PopupMenuItem(value: 'restart', child: Text('重启')),
                    const PopupMenuItem(value: 'remove', child: Text('删除')),
                  ],
                ),
                onTap: () => _showDetails(container),
              ),
            ),
        ],
      ),
    );
  }

  String _subtitle(ContainerInfo container) {
    final parts = [
      if (container.ipv4.isNotEmpty) container.ipv4,
      if (container.memory.isNotEmpty) container.memory,
      if (container.network.isNotEmpty) container.network,
    ];
    if (parts.isEmpty) return container.status;
    return parts.join(' · ');
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
