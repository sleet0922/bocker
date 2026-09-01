import 'package:flutter/material.dart';

import 'models.dart';
import 'ssh_service.dart';
import 'widgets.dart';

class TemplatesPage extends StatefulWidget {
  const TemplatesPage({super.key, required this.service});

  final SshService service;

  @override
  State<TemplatesPage> createState() => TemplatesPageState();
}

class TemplatesPageState extends State<TemplatesPage> {
  List<ImageTemplate> _templates = const [];
  final Set<String> _distros = {};
  String? _distro;
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
      const ['template', 'list', '--json'],
      timeout: const Duration(seconds: 120),
    );
    if (!mounted) return;
    setState(() {
      _loading = false;
      if (result.ok) {
        _templates = parseImageTemplates(result.output);
        _distros
          ..clear()
          ..addAll(_templates.map((item) => item.distro));
        if (_distro == null || !_distros.contains(_distro)) {
          _distro = _distros.isEmpty ? null : _distros.first;
        }
      } else {
        _error = result.summary;
      }
    });
  }

  Future<void> _install(ImageTemplate template) async {
    final nameController = TextEditingController();
    var network = 'nat';
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => StatefulBuilder(
        builder: (dialogContext, setDialogState) => AlertDialog(
          title: Text('安装 ${template.distro} ${template.release}'),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                '将下载模板 ${template.image}，创建并启动容器。'
                '下载可能需要几分钟，请保持网络畅通。',
                style: TextStyle(
                  color: Theme.of(dialogContext).colorScheme.onSurfaceVariant,
                ),
              ),
              const SizedBox(height: 16),
              TextField(
                controller: nameController,
                decoration: const InputDecoration(
                  labelText: '容器名称',
                  hintText: '留空则由 Bocker 自动生成',
                ),
              ),
              const SizedBox(height: 16),
              const Text('网络模式'),
              const SizedBox(height: 8),
              SegmentedButton<String>(
                showSelectedIcon: false,
                segments: const [
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
              child: const Text('安装'),
            ),
          ],
        ),
      ),
    );
    final name = nameController.text.trim();
    nameController.dispose();
    if (confirmed != true) return;
    if (!mounted) return;
    if (name.isNotEmpty && !isValidBockerName(name)) {
      showResultSnackBar(
        context,
        const CommandResult(
          false,
          '容器名称需为 1-63 位字母、数字或连字符，且不能以连字符开头或结尾',
          -1,
        ),
        successLabel: '安装模板',
      );
      return;
    }
    final result = await runWithProgressDialog(
      context,
      widget.service,
      '安装模板',
      [
        'template',
        'install',
        template.image,
        '--network',
        network,
        if (name.isNotEmpty) ...['--name', name],
      ],
      timeout: const Duration(minutes: 30),
    );
    if (!mounted) return;
    showResultSnackBar(context, result, successLabel: '安装模板');
  }

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    if (_loading && _templates.isEmpty && _error == null) {
      return const Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            CircularProgressIndicator(),
            SizedBox(height: 12),
            Text('正在从镜像源获取模板，可能需要十几秒…'),
          ],
        ),
      );
    }
    if (_error != null && _templates.isEmpty) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(Icons.cloud_off_outlined, size: 56, color: colors.error),
              const SizedBox(height: 12),
              Text('获取模板失败', style: Theme.of(context).textTheme.titleMedium),
              const SizedBox(height: 4),
              Text(
                _error!,
                textAlign: TextAlign.center,
                style: TextStyle(color: colors.onSurfaceVariant),
              ),
              const SizedBox(height: 16),
              FilledButton.icon(
                onPressed: refresh,
                icon: const Icon(Icons.refresh),
                label: const Text('重试'),
              ),
            ],
          ),
        ),
      );
    }
    final selected = _templates
        .where((item) => item.distro == _distro)
        .toList();
    return Column(
      children: [
        SizedBox(
          width: double.infinity,
          child: SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
            child: Row(
              children: [
                for (final distro in _distros)
                  Padding(
                    padding: const EdgeInsets.only(right: 8),
                    child: FilterChip(
                      label: Text(distro),
                      selected: distro == _distro,
                      onSelected: (_) => setState(() => _distro = distro),
                    ),
                  ),
              ],
            ),
          ),
        ),
        Expanded(
          child: RefreshIndicator(
            onRefresh: refresh,
            child: selected.isEmpty
                ? ListView(
                    physics: const AlwaysScrollableScrollPhysics(),
                    children: const [
                      Padding(
                        padding: EdgeInsets.only(top: 80),
                        child: Center(child: Text('该发行版暂无可用模板')),
                      ),
                    ],
                  )
                : ListView(
                    physics: const AlwaysScrollableScrollPhysics(),
                    padding: const EdgeInsets.fromLTRB(12, 0, 12, 16),
                    children: [
                      for (final template in selected)
                        Card(
                          margin: const EdgeInsets.only(bottom: 8),
                          child: ListTile(
                            leading: const Icon(Icons.system_update_alt),
                            title: Text(
                              '${template.distro} ${template.release}',
                              style: const TextStyle(
                                fontWeight: FontWeight.w600,
                              ),
                            ),
                            subtitle: Text(template.image),
                            trailing: FilledButton.tonalIcon(
                              onPressed: () => _install(template),
                              icon: const Icon(Icons.download_outlined),
                              label: const Text('安装'),
                            ),
                          ),
                        ),
                    ],
                  ),
          ),
        ),
      ],
    );
  }
}
