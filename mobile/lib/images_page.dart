import 'package:flutter/material.dart';

import 'models.dart' as models;
import 'ssh_service.dart';
import 'widgets.dart';

class ImagesPage extends StatefulWidget {
  const ImagesPage({super.key, required this.service});

  final SshService service;

  @override
  State<ImagesPage> createState() => ImagesPageState();
}

class ImagesPageState extends State<ImagesPage> {
  List<models.ImageInfo> _images = const [];
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
      const ['image', 'list', '--json'],
      timeout: const Duration(seconds: 60),
    );
    if (!mounted) return;
    setState(() {
      _loading = false;
      if (result.ok) {
        _images = models.parseImages(result.output);
      } else {
        _error = result.summary;
      }
    });
  }

  Future<void> _runAction(
    String label,
    List<String> args, {
    Duration timeout = const Duration(seconds: 300),
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

  Future<void> _runImage(models.ImageInfo image) async {
    final defaultName = image.name.length <= 61
        ? '${image.name}-1'
        : '${image.name.substring(0, 61)}-1';
    final nameController = TextEditingController(text: defaultName);
    var network = 'default';
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => StatefulBuilder(
        builder: (dialogContext, setDialogState) => AlertDialog(
          title: Text('运行镜像 ${image.name}'),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              TextField(
                controller: nameController,
                autofocus: true,
                decoration: const InputDecoration(labelText: '容器名称'),
              ),
              const SizedBox(height: 16),
              Align(
                alignment: Alignment.centerLeft,
                child: Text(
                  '网络模式',
                  style: TextStyle(
                    color: Theme.of(dialogContext).colorScheme.onSurfaceVariant,
                    fontSize: 13,
                  ),
                ),
              ),
              const SizedBox(height: 8),
              SegmentedButton<String>(
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
            ],
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.of(dialogContext).pop(false),
              child: const Text('取消'),
            ),
            FilledButton(
              onPressed: () => Navigator.of(dialogContext).pop(true),
              child: const Text('创建并启动'),
            ),
          ],
        ),
      ),
    );
    final name = nameController.text.trim();
    nameController.dispose();
    if (confirmed != true) return;
    if (!mounted) return;
    if (!models.isValidBockerName(name)) {
      showResultSnackBar(
        context,
        const models.CommandResult(
          false,
          '容器名称需为 1-63 位字母、数字或连字符，且不能以连字符开头或结尾',
          -1,
        ),
        successLabel: '运行镜像',
      );
      return;
    }
    await _runAction('运行镜像', [
      'image',
      'run',
      image.name,
      '--name',
      name,
      if (network != 'default') ...['--network', network],
    ]);
  }

  void _confirmRemove(models.ImageInfo image) {
    showDialog<void>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: Text('删除镜像 ${image.name}'),
        content: const Text('确定删除这个本地镜像吗？'),
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
              _runAction('删除镜像', ['image', 'remove', image.name]);
            },
            child: const Text('删除'),
          ),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    if (_loading && _images.isEmpty && _error == null) {
      return const Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            CircularProgressIndicator(),
            SizedBox(height: 12),
            Text('正在获取镜像列表…'),
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
          if (_images.isEmpty && _error == null)
            Padding(
              padding: const EdgeInsets.only(top: 80),
              child: Column(
                children: [
                  Icon(
                    Icons.layers_outlined,
                    size: 56,
                    color: colors.onSurfaceVariant,
                  ),
                  const SizedBox(height: 12),
                  const Text('暂无本地镜像'),
                  const SizedBox(height: 4),
                  Text(
                    '本地镜像由 Incusfile 构建产生',
                    style: TextStyle(color: colors.onSurfaceVariant),
                  ),
                ],
              ),
            ),
          for (final image in _images)
            Card(
              margin: const EdgeInsets.only(bottom: 8),
              child: ListTile(
                leading: const Icon(Icons.image_outlined),
                title: Text(
                  image.name,
                  style: const TextStyle(fontWeight: FontWeight.w600),
                ),
                subtitle: Text(
                  [
                    if (image.size.isNotEmpty) image.size,
                    if (image.created.isNotEmpty) image.created,
                  ].join(' · '),
                ),
                trailing: PopupMenuButton<String>(
                  onSelected: (action) {
                    if (action == 'run') _runImage(image);
                    if (action == 'remove') _confirmRemove(image);
                  },
                  itemBuilder: (_) => const [
                    PopupMenuItem(value: 'run', child: Text('运行')),
                    PopupMenuItem(value: 'remove', child: Text('删除')),
                  ],
                ),
              ),
            ),
        ],
      ),
    );
  }
}
