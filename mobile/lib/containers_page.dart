import 'package:flutter/material.dart';

import 'container_detail_page.dart';
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

  Future<void> _openDetails(ContainerInfo container) async {
    await Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => ContainerDetailPage(
          service: widget.service,
          containerName: container.name,
        ),
      ),
    );
    refresh();
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
                onTap: () => _openDetails(container),
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
