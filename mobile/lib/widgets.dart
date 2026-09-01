import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';

import 'models.dart';
import 'ssh_service.dart';

/// 截取输出尾部，避免长时间命令的输出撑爆界面。
String tailOutput(String text, {int limit = 6000}) {
  if (text.length <= limit) return text;
  return '……（已截断）\n${text.substring(text.length - limit)}';
}

/// 执行远程命令，并弹出不可关闭的进度对话框实时展示输出。
Future<CommandResult> runWithProgressDialog(
  BuildContext context,
  SshService service,
  String label,
  List<String> args, {
  Duration? timeout,
}) {
  final progress = ValueNotifier<String>('');
  final taskDone = Completer<void>();
  CommandResult? result;

  unawaited(
    service
        .run(args, timeout: timeout, onOutput: (text) => progress.value = text)
        .then((value) {
          result = value;
        })
        .whenComplete(() => taskDone.complete()),
  );

  unawaited(
    showDialog<void>(
      context: context,
      barrierDismissible: false,
      builder: (dialogContext) => _CommandProgressDialog(
        label: label,
        progress: progress,
        taskDone: taskDone.future,
      ),
    ),
  );

  return taskDone.future.then((_) => result!);
}

void showResultSnackBar(
  BuildContext context,
  CommandResult result, {
  required String successLabel,
}) {
  final colors = Theme.of(context).colorScheme;
  ScaffoldMessenger.of(context).showSnackBar(
    SnackBar(
      content: Text(
        result.ok ? '$successLabel 已完成' : '$successLabel 失败：${result.summary}',
      ),
      behavior: SnackBarBehavior.floating,
      backgroundColor: result.ok ? null : colors.error,
    ),
  );
}

class _CommandProgressDialog extends StatefulWidget {
  const _CommandProgressDialog({
    required this.label,
    required this.progress,
    required this.taskDone,
  });

  final String label;
  final ValueListenable<String> progress;
  final Future<void> taskDone;

  @override
  State<_CommandProgressDialog> createState() => _CommandProgressDialogState();
}

class _CommandProgressDialogState extends State<_CommandProgressDialog> {
  @override
  void initState() {
    super.initState();
    widget.taskDone.then((_) {
      if (mounted) Navigator.of(context).pop();
    });
  }

  @override
  Widget build(BuildContext context) {
    return PopScope(
      canPop: false,
      child: AlertDialog(
        title: Text(widget.label),
        content: SizedBox(
          width: double.maxFinite,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              const LinearProgressIndicator(),
              const SizedBox(height: 12),
              Flexible(
                child: SingleChildScrollView(
                  reverse: true,
                  child: ValueListenableBuilder<String>(
                    valueListenable: widget.progress,
                    builder: (context, text, _) => SelectableText(
                      text.isEmpty ? '正在执行，请稍候…' : tailOutput(text),
                      style: const TextStyle(
                        fontFamily: 'monospace',
                        fontSize: 12,
                        height: 1.4,
                      ),
                    ),
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
