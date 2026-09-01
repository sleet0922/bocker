import 'package:flutter/material.dart';

import 'containers_page.dart';
import 'images_page.dart';
import 'settings_page.dart';
import 'ssh_service.dart';
import 'templates_page.dart';

class HomePage extends StatefulWidget {
  const HomePage({super.key, required this.service});

  final SshService service;

  @override
  State<HomePage> createState() => _HomePageState();
}

class _HomePageState extends State<HomePage> {
  int _index = 0;
  final _containersKey = GlobalKey<ContainersPageState>();
  final _imagesKey = GlobalKey<ImagesPageState>();
  final _templatesKey = GlobalKey<TemplatesPageState>();

  static const _titles = ['容器', '镜像', '模板', '设置'];

  void _select(int index) {
    setState(() => _index = index);
    // 容器/镜像是本地查询，切换时自动刷新；模板走网络镜像源，仅手动刷新。
    if (index == 0) _containersKey.currentState?.refresh();
    if (index == 1) _imagesKey.currentState?.refresh();
  }

  @override
  Widget build(BuildContext context) {
    final endpoint = '${widget.service.username}@${widget.service.host}';
    return Scaffold(
      appBar: AppBar(
        title: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(_titles[_index]),
            Text(
              endpoint,
              style: Theme.of(
                context,
              ).textTheme.bodySmall?.copyWith(fontFeatures: []),
            ),
          ],
        ),
        actions: [
          if (_index == 0)
            IconButton(
              tooltip: '刷新',
              icon: const Icon(Icons.refresh),
              onPressed: () => _containersKey.currentState?.refresh(),
            ),
          if (_index == 1)
            IconButton(
              tooltip: '刷新',
              icon: const Icon(Icons.refresh),
              onPressed: () => _imagesKey.currentState?.refresh(),
            ),
          if (_index == 2)
            IconButton(
              tooltip: '刷新',
              icon: const Icon(Icons.refresh),
              onPressed: () => _templatesKey.currentState?.refresh(),
            ),
        ],
      ),
      body: IndexedStack(
        index: _index,
        children: [
          ContainersPage(key: _containersKey, service: widget.service),
          ImagesPage(key: _imagesKey, service: widget.service),
          TemplatesPage(key: _templatesKey, service: widget.service),
          SettingsPage(service: widget.service),
        ],
      ),
      bottomNavigationBar: NavigationBar(
        selectedIndex: _index,
        onDestinationSelected: _select,
        destinations: const [
          NavigationDestination(
            icon: Icon(Icons.view_in_ar_outlined),
            selectedIcon: Icon(Icons.view_in_ar),
            label: '容器',
          ),
          NavigationDestination(
            icon: Icon(Icons.layers_outlined),
            selectedIcon: Icon(Icons.layers),
            label: '镜像',
          ),
          NavigationDestination(
            icon: Icon(Icons.cloud_download_outlined),
            selectedIcon: Icon(Icons.cloud_download),
            label: '模板',
          ),
          NavigationDestination(
            icon: Icon(Icons.settings_outlined),
            selectedIcon: Icon(Icons.settings),
            label: '设置',
          ),
        ],
      ),
    );
  }
}
