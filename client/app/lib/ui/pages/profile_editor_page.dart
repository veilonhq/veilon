import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart' show ScrollCacheExtent;

import '../../core/app_state.dart';
import '../../core/errors.dart';
import '../../core/models.dart';
import '../../core/profile_schema.dart';
import '../../l10n/strings.dart';
import '../../theme/palette.dart';
import '../widgets/glass.dart';
import '../widgets/proto_badge.dart';
import '../widgets/sub_scaffold.dart';

/// Full editor for one profile: every core field grouped per protocol, plus
/// a raw-JSON section for anything the form does not cover.
class ProfileEditorPage extends StatefulWidget {
  const ProfileEditorPage({super.key, required this.profile, this.isNew = false});
  final Profile profile;

  /// "Add manually": an empty profile; the protocol list is [manualTypes]
  /// (Xray / sing-box only, no AmneziaWG).
  final bool isNew;

  static Future<Profile?> open(BuildContext context, Profile p) =>
      Navigator.of(context).push<Profile>(MaterialPageRoute(builder: (_) => ProfileEditorPage(profile: p)));

  /// Opens the editor on a new, empty VLESS profile.
  static Future<Profile?> openNew(BuildContext context) => Navigator.of(context).push<Profile>(MaterialPageRoute(
      builder: (_) => ProfileEditorPage(profile: Profile(<String, dynamic>{'type': 'vless'}), isNew: true)));

  @override
  State<ProfileEditorPage> createState() => _ProfileEditorPageState();
}

class _ProfileEditorPageState extends State<ProfileEditorPage> {
  final _form = GlobalKey<FormState>();
  late Map<String, dynamic> _base; // JSON the values apply onto
  late Vals _vals;
  final Map<String, TextEditingController> _ctl = {};
  final Set<String> _revealed = {};
  final _raw = TextEditingController();
  String? _rawError;
  bool _saving = false;
  int _gen = 0; // bumps when values are reloaded (raw JSON applied)

  @override
  void initState() {
    super.initState();
    _base = deepCopy(widget.profile.json);
    _vals = valuesOf(_base, widget.profile.type);
  }

  @override
  void dispose() {
    for (final c in _ctl.values) {
      c.dispose();
    }
    _raw.dispose();
    super.dispose();
  }

  String get _type => _vals['type'] ?? '';

  TextEditingController _c(String path) => _ctl.putIfAbsent(path, () => TextEditingController(text: _vals[path] ?? ''));

  void _setType(String t) {
    setState(() {
      // Fields of the new type that are not loaded yet come from the JSON.
      final fresh = valuesOf(applyValues(_base, _vals), t);
      fresh.forEach((k, v) => _vals.putIfAbsent(k, () => v));
      _vals['type'] = t;
    });
  }

  Map<String, dynamic> _current() => applyValues(_base, _vals);

  void _reload(Map<String, dynamic> j) {
    setState(() {
      _base = j;
      _vals = valuesOf(j, j['type'] as String? ?? _type);
      final old = _ctl.values.toList();
      _ctl.clear();
      _gen++;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        for (final c in old) {
          c.dispose();
        }
      });
    });
  }

  Future<void> _save() async {
    final s = S.of(context);
    final fa = s.isFa;
    final okForm = _form.currentState?.validate() ?? false;
    final errs = validateAll(_vals);
    if (!okForm || errs.isNotEmpty) {
      showToast(context, fa ? 'لطفاً خطاهای فرم را برطرف کنید' : 'Please fix the highlighted fields', error: true);
      return;
    }
    final app = AppScope.read(context);
    setState(() => _saving = true);
    final (j, normErr) = await app.normalizeEdited(_current());
    if (!mounted) return;
    if (normErr != null) {
      setState(() => _saving = false);
      showToast(context, normErr, error: true);
      return;
    }
    String coreErr = '';
    try {
      coreErr = await app.checkProfileJson(jsonEncode(j));
    } catch (_) {}
    if (!mounted) return;
    setState(() => _saving = false);
    if (coreErr.isNotEmpty) {
      final go = await showDialog<bool>(
        context: context,
        builder: (ctx) => AlertDialog(
          title: Text(fa ? 'هسته این پیکربندی را نپذیرفت' : 'The core rejected this config'),
          content: Text(friendlyErrorKey(coreErr).startsWith('err.friendly.') ? coreErr : s.t(friendlyErrorKey(coreErr)),
              style: const TextStyle(color: Palette.warn, fontSize: 13)),
          actions: [
            TextButton(onPressed: () => Navigator.pop(ctx, false), child: Text(s.t('common.cancel'))),
            FilledButton(onPressed: () => Navigator.pop(ctx, true), child: Text(fa ? 'ذخیره در هر حال' : 'Save anyway')),
          ],
        ),
      );
      if (go != true || !mounted) return;
    }
    final changed = connectionChanged(widget.profile.json, j);
    final saved = await app.saveProfile(widget.profile, j, connectionChanged: changed);
    if (!mounted) return;
    final stale = app.isStaleConnection(saved) && (changed || j['addressOverride'] != widget.profile.json['addressOverride']);
    showToast(
        context,
        stale
            ? (fa ? 'ذخیره شد — برای اعمال، دوباره وصل شوید' : 'Saved — reconnect to apply')
            : (fa ? 'ذخیره شد' : 'Saved'));
    Navigator.of(context).maybePop(saved);
  }

  @override
  Widget build(BuildContext context) {
    final s = S.of(context);
    final fa = s.isFa;
    final groups = groupsFor(_type);
    return SubScaffold(
      title: widget.isNew ? S.of(context).t('add.manualTitle') : (fa ? 'ویرایش سرور' : 'Edit server'),
      actions: [
        GlassIconButton(
          icon: Icons.check_rounded,
          color: Palette.lime,
          busy: _saving,
          tooltip: s.t('common.save'),
          onTap: _save,
        ),
      ],
      bottom: Padding(
        padding: const EdgeInsets.fromLTRB(Gap.page, Gap.s, Gap.page, Gap.m),
        child: PrimaryButton(label: s.t('common.save'), icon: Icons.save_rounded, busy: _saving, onTap: _save),
      ),
      child: Form(
        key: _form,
        autovalidateMode: AutovalidateMode.onUserInteraction,
        child: ListView(
          key: ValueKey(_gen),
          // 1.0.7: each field group is one big list child; building one when
          // it scrolled into view (a dozen text fields at once) cost a long
          // frame. The whole form is built up front instead (it is short).
          scrollCacheExtent: const ScrollCacheExtent.pixels(4000),
          padding: const EdgeInsets.fromLTRB(Gap.page, 0, Gap.page, Gap.xl),
          children: [
            _header(fa),
            for (final g in groups)
              if (g.shown(_vals)) _group(g, fa),
            _rawSection(fa),
          ],
        ),
      ),
    );
  }

  Widget _header(bool fa) {
    final p = Profile(_current());
    return Padding(
      padding: const EdgeInsets.only(top: Gap.s),
      child: Glass(
        radius: Radii.l,
        padding: const EdgeInsets.all(Gap.m),
        child: Row(children: [
          ProtoBadge(type: _type, size: 40),
          const SizedBox(width: Gap.m),
          Expanded(
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text(p.name, maxLines: 1, overflow: TextOverflow.ellipsis, style: const TextStyle(fontWeight: FontWeight.w800)),
              const SizedBox(height: 2),
              Text(p.label, style: const TextStyle(color: Palette.muted, fontSize: 12)),
            ]),
          ),
          if (widget.profile.subId != null)
            Pill(text: fa ? 'اشتراک' : 'Subscription', color: Palette.violetFg, icon: Icons.cloud_sync_rounded),
        ]),
      ),
    );
  }

  Widget _group(FieldGroup g, bool fa) {
    final fields = g.fields.where((f) => f.shown(_vals)).toList();
    return Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
      SectionTitle(g.label(fa)),
      Glass(
        radius: Radii.l,
        padding: const EdgeInsets.fromLTRB(Gap.m, Gap.m, Gap.m, Gap.xs),
        child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
          for (final f in fields)
            Padding(padding: const EdgeInsets.only(bottom: Gap.m), child: _field(f, fa)),
        ]),
      ),
    ]);
  }

  String? _validator(FieldSpec f, String? v, bool fa) {
    final e = validateField(f, v ?? '');
    return e == null ? null : errorText(e, fa);
  }

  Widget _field(FieldSpec f, bool fa) {
    final key = ValueKey('field:${f.path}');
    switch (f.kind) {
      case FKind.toggle:
        return Material(
          key: key,
          color: Palette.glass,
          shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(Radii.m), side: const BorderSide(color: Palette.line)),
          clipBehavior: Clip.antiAlias,
          child: SwitchListTile(
            dense: true,
            title: Text(f.label(fa), style: const TextStyle(fontSize: 14)),
            subtitle: f.hint(fa).isEmpty ? null : Text(f.hint(fa), style: const TextStyle(color: Palette.muted, fontSize: 12)),
            value: _vals[f.path] == 'true',
            shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(Radii.m)),
            onChanged: (v) => setState(() => _vals[f.path] = v ? 'true' : 'false'),
          ),
        );
      case FKind.choice:
        final cur = _vals[f.path] ?? '';
        final items = [...(f.path == 'type' && widget.isNew ? manualTypes : f.choices)];
        if (!items.contains(cur)) items.insert(0, cur);
        return DropdownButtonFormField<String>(
          key: key,
          initialValue: cur,
          isExpanded: true,
          dropdownColor: Palette.bg3,
          borderRadius: BorderRadius.circular(Radii.m),
          decoration: _deco(f, fa),
          items: [
            for (final c in items)
              DropdownMenuItem(
                value: c,
                child: Text(
                  c.isEmpty
                      ? (f.emptyLabel(fa).isNotEmpty ? f.emptyLabel(fa) : (fa ? 'پیش فرض' : 'Default'))
                      : (f.path == 'type' ? Profile.protocolName(c) : (f.choiceLabels[c] ?? c)),
                  textDirection: c.isEmpty ? null : TextDirection.ltr,
                ),
              ),
          ],
          validator: (v) => _validator(f, v, fa),
          onChanged: (v) {
            if (v == null) return;
            if (f.path == 'type') {
              _setType(v);
            } else if (f.path == 'network') {
              setState(() {
                _vals['network'] = v;
                onNetworkChanged(_vals);
              });
            } else {
              setState(() => _vals[f.path] = v);
            }
          },
        );
      case FKind.maskList:
        return _MaskListField(
          key: key,
          spec: f,
          fa: fa,
          value: _vals[f.path] ?? '',
          onChanged: (v) => setState(() => _vals[f.path] = v),
        );
      default:
        final multi = f.kind == FKind.multiline || f.kind == FKind.list || f.kind == FKind.intList;
        final secret = f.kind == FKind.secret;
        final hidden = secret && !_revealed.contains(f.path);
        final numeric = f.kind == FKind.integer || f.kind == FKind.decimal;
        return TextFormField(
          key: key,
          controller: _c(f.path),
          obscureText: hidden,
          minLines: multi ? (f.kind == FKind.multiline ? 3 : 2) : 1,
          maxLines: multi ? 8 : 1,
          autocorrect: false,
          enableSuggestions: false,
          keyboardType: numeric
              ? TextInputType.numberWithOptions(decimal: f.kind == FKind.decimal)
              : (multi ? TextInputType.multiline : TextInputType.text),
          // Technical values are always left-to-right, even in the Persian UI.
          textDirection: f.path == 'name' ? null : TextDirection.ltr,
          style: TextStyle(fontSize: 14, fontFamily: f.path == 'name' ? null : 'monospace', fontFamilyFallback: const [Fonts.fa]),
          decoration: _deco(f, fa).copyWith(
            suffixIcon: secret
                ? IconButton(
                    icon: Icon(hidden ? Icons.visibility_rounded : Icons.visibility_off_rounded, size: 20, color: Palette.muted),
                    onPressed: () => setState(() => hidden ? _revealed.add(f.path) : _revealed.remove(f.path)),
                  )
                : null,
          ),
          validator: (v) => _validator(f, v, fa),
          onChanged: (v) {
            // Rebuild: the header preview and field visibility can depend on it.
            setState(() => _vals[f.path] = v);
          },
        );
    }
  }

  InputDecoration _deco(FieldSpec f, bool fa) => InputDecoration(
        labelText: f.required ? '${f.label(fa)} *' : f.label(fa),
        helperText: f.hint(fa).isEmpty ? null : f.hint(fa),
        helperMaxLines: 3,
        errorMaxLines: 3,
        labelStyle: const TextStyle(color: Palette.text2),
        helperStyle: const TextStyle(color: Palette.muted, fontSize: 11.5),
      );

  Widget _rawSection(bool fa) {
    return Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
      SectionTitle(fa ? 'پیشرفته: JSON کامل' : 'Advanced: full JSON'),
      Glass(
        radius: Radii.l,
        padding: const EdgeInsets.all(Gap.m),
        child: Theme(
          data: Theme.of(context).copyWith(dividerColor: Colors.transparent),
          child: ExpansionTile(
            key: const ValueKey('rawJson'),
            tilePadding: EdgeInsets.zero,
            childrenPadding: EdgeInsets.zero,
            title: Text(fa ? 'ویرایش همه فیلدها به صورت JSON' : 'Edit every field as JSON',
                style: const TextStyle(fontSize: 14, fontWeight: FontWeight.w600)),
            subtitle: Text(
                fa ? 'برای فیلدهایی که در فرم بالا نیستند' : 'For anything the form above does not cover',
                style: const TextStyle(color: Palette.muted, fontSize: 12)),
            onExpansionChanged: (open) {
              if (open) {
                _raw.text = const JsonEncoder.withIndent('  ').convert(_current());
                _rawError = null;
              }
            },
            children: [
              TextField(
                controller: _raw,
                minLines: 8,
                maxLines: 20,
                textDirection: TextDirection.ltr,
                autocorrect: false,
                enableSuggestions: false,
                style: const TextStyle(fontFamily: 'monospace', fontSize: 12.5),
                decoration: InputDecoration(errorText: _rawError),
              ),
              const SizedBox(height: Gap.s),
              Align(
                alignment: AlignmentDirectional.centerEnd,
                child: OutlinedButton.icon(
                  icon: const Icon(Icons.data_object_rounded, size: 18),
                  label: Text(fa ? 'اعمال JSON' : 'Apply JSON'),
                  onPressed: _applyRaw,
                ),
              ),
            ],
          ),
        ),
      ),
    ]);
  }

  void _applyRaw() {
    final fa = S.of(context).isFa;
    try {
      final v = jsonDecode(_raw.text);
      if (v is! Map) throw const FormatException();
      final j = Map<String, dynamic>.from(v);
      final t = j['type'];
      if (t is! String || !profileTypes.contains(t)) {
        setState(() => _rawError = fa ? 'فیلد type نامعتبر است' : 'Invalid or missing "type"');
        return;
      }
      j['id'] = widget.profile.id;
      _rawError = null;
      _reload(j);
      showToast(context, fa ? 'JSON اعمال شد' : 'JSON applied');
    } catch (_) {
      setState(() => _rawError = errorText(('json', null, null), fa));
    }
  }
}


/// Editable list of FinalMask layers: each row has a type and its settings
/// (JSON object); rows can be added, removed and reordered.
class _MaskListField extends StatefulWidget {
  const _MaskListField({super.key, required this.spec, required this.fa, required this.value, required this.onChanged});
  final FieldSpec spec;
  final bool fa;
  final String value;
  final ValueChanged<String> onChanged;

  @override
  State<_MaskListField> createState() => _MaskListFieldState();
}

class _MaskRow {
  _MaskRow(String type, String settings)
      : type = TextEditingController(text: type),
        settings = TextEditingController(text: settings);
  final TextEditingController type;
  final TextEditingController settings;
  void dispose() {
    type.dispose();
    settings.dispose();
  }
}

class _MaskListFieldState extends State<_MaskListField> {
  final List<_MaskRow> _rows = [];

  @override
  void initState() {
    super.initState();
    for (final m in decodeMaskVals(widget.value) ?? const <Map<String, String>>[]) {
      _rows.add(_MaskRow(m['type'] ?? '', m['settings'] ?? ''));
    }
  }

  @override
  void dispose() {
    for (final r in _rows) {
      r.dispose();
    }
    super.dispose();
  }

  void _emit() => widget.onChanged(encodeMaskVals([
        for (final r in _rows) {'type': r.type.text, 'settings': r.settings.text},
      ]));

  void _add() {
    setState(() => _rows.add(_MaskRow(widget.spec.maskTypes.isNotEmpty ? widget.spec.maskTypes.first : '', '')));
    _emit();
  }

  void _remove(int i) {
    final r = _rows.removeAt(i);
    setState(() {});
    WidgetsBinding.instance.addPostFrameCallback((_) => r.dispose());
    _emit();
  }

  void _move(int i, int d) {
    final j = i + d;
    if (j < 0 || j >= _rows.length) return;
    setState(() {
      final r = _rows.removeAt(i);
      _rows.insert(j, r);
    });
    _emit();
  }

  @override
  Widget build(BuildContext context) {
    final fa = widget.fa;
    final f = widget.spec;
    return Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
      Row(children: [
        Expanded(child: Text(f.label(fa), style: const TextStyle(color: Palette.text2, fontWeight: FontWeight.w600))),
        Pill(text: '${_rows.length}', color: Palette.violetFg),
      ]),
      if (f.hint(fa).isNotEmpty)
        Padding(
          padding: const EdgeInsets.only(top: 2, bottom: Gap.s),
          child: Text(f.hint(fa), style: const TextStyle(color: Palette.muted, fontSize: 11.5)),
        ),
      for (var i = 0; i < _rows.length; i++)
        Container(
          key: ObjectKey(_rows[i]),
          margin: const EdgeInsets.only(bottom: Gap.s),
          padding: const EdgeInsets.all(Gap.s),
          decoration: BoxDecoration(
            borderRadius: BorderRadius.circular(Radii.m),
            border: Border.all(color: Palette.line2),
          ),
          child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
            Row(children: [
              Text(fa ? 'لایه ${i + 1}' : 'Layer ${i + 1}',
                  style: const TextStyle(color: Palette.lime, fontWeight: FontWeight.w700, fontSize: 12.5)),
              const Spacer(),
              IconButton(
                  visualDensity: VisualDensity.compact,
                  tooltip: fa ? 'بالا' : 'Up',
                  icon: const Icon(Icons.arrow_upward_rounded, size: 18),
                  onPressed: i == 0 ? null : () => _move(i, -1)),
              IconButton(
                  visualDensity: VisualDensity.compact,
                  tooltip: fa ? 'پایین' : 'Down',
                  icon: const Icon(Icons.arrow_downward_rounded, size: 18),
                  onPressed: i == _rows.length - 1 ? null : () => _move(i, 1)),
              IconButton(
                  visualDensity: VisualDensity.compact,
                  tooltip: fa ? 'حذف' : 'Remove',
                  icon: const Icon(Icons.delete_outline_rounded, size: 18, color: Palette.danger),
                  onPressed: () => _remove(i)),
            ]),
            TextFormField(
              controller: _rows[i].type,
              textDirection: TextDirection.ltr,
              autocorrect: false,
              decoration: InputDecoration(
                labelText: fa ? 'نوع *' : 'Type *',
                helperText: f.maskTypes.join(' · '),
                helperMaxLines: 2,
                helperStyle: const TextStyle(color: Palette.muted, fontSize: 11),
              ),
              validator: (v) => (v ?? '').trim().isEmpty ? errorText(('maskType', null, null), fa) : null,
              onChanged: (_) => _emit(),
            ),
            const SizedBox(height: Gap.s),
            TextFormField(
              controller: _rows[i].settings,
              textDirection: TextDirection.ltr,
              autocorrect: false,
              enableSuggestions: false,
              minLines: 2,
              maxLines: 8,
              style: const TextStyle(fontFamily: 'monospace', fontSize: 12.5, fontFamilyFallback: [Fonts.fa]),
              decoration: InputDecoration(labelText: fa ? 'تنظیمات (JSON)' : 'Settings (JSON)', hintText: '{}'),
              validator: (v) => (v ?? '').trim().isEmpty || checkJsonObject(v!.trim()) == null
                  ? null
                  : errorText(('json', null, null), fa),
              onChanged: (_) => _emit(),
            ),
          ]),
        ),
      Align(
        alignment: AlignmentDirectional.centerStart,
        child: OutlinedButton.icon(
          key: ValueKey('add:${f.path}'),
          icon: const Icon(Icons.add_rounded, size: 18),
          label: Text(fa ? 'افزودن ماسک' : 'Add mask'),
          onPressed: _add,
        ),
      ),
    ]);
  }
}
