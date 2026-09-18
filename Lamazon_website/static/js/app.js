// Uniminute storefront — Alpine stores + HTMX hooks.
// Loaded with defer before alpine/cdn.min.js so alpine:init fires first.

document.addEventListener('alpine:init', () => {

  // ── drawers store ────────────────────────────────────────────────────────
  // Used by: header (mobile-menu button), cart (bottom nav Cart tab),
  // ui.templ Drawer component, MobileMenu component.
  Alpine.store('drawers', {
    current: null,

    open(id) {
      this.current = id;
      document.body.classList.add('drawer-open');
      // Notify listeners (e.g. the cart drawer that needs to refresh).
      document.dispatchEvent(new CustomEvent('lw:drawer:open', { detail: { id } }));
    },

    close(id) {
      if (this.current === id) {
        this.current = null;
        document.body.classList.remove('drawer-open');
      }
    },

    closeAll() {
      this.current = null;
      document.body.classList.remove('drawer-open');
    },

    isOpen(id) {
      return this.current === id;
    },
  });

  // ── toasts component ─────────────────────────────────────────────────────
  // Used by ui.templ ToastRegion. The server emits HX-Trigger: {"lw:toast":
  // {"message":"…","undo":false}} and the HTMX hook below dispatches that
  // onto the window so this component catches it.
  Alpine.data('toasts', () => ({
    toasts: [],
    _next: 1,

    init() {
      window.addEventListener('lw:toast', (e) => {
        const { message, title, added, basket, undo, undoLabel, undoAction, reload, tone } = e.detail || {};
        if (!message) return;
        const id = this._next++;
        // One at a time, like hideCurrentSnackBar before every show.
        this.toasts = [{
          id,
          message,
          title,
          added: !!added,
          basket: !!basket,
          undo: !!undo,
          undoLabel: undoLabel || 'Undo',
          undoAction,
          reload: !!reload,
          tone: tone || '',
          visible: true,
        }];
        // 1.4s for the added card, 3s (5s with Undo) for a message.
        setTimeout(() => this.dismiss(id), added ? 1400 : undo ? 5000 : 3000);
      });
    },

    dismiss(id) {
      const t = this.toasts.find(t => t.id === id);
      if (t) t.visible = false;
      setTimeout(() => {
        this.toasts = this.toasts.filter(t => t.id !== id);
      }, 250);
    },

    undo(t) {
      if (t.undoAction) {
        // Cart.putBack, then show the basket it restored.
        htmx.ajax('POST', t.undoAction, { swap: 'none' }).then(() => {
          if (t.reload) location.reload();
        });
      }
      this.dismiss(t.id);
    },
  }));

  // ── campaign deck (CampaignDeck) ─────────────────────────────────────────
  // Advances on its own after each banner's dwell (6s, 12s for a clip); an
  // arrow press re-arms the timer; reduced motion never advances it.
  Alpine.data('deck', (count, dwells) => ({
    index: 0,
    timer: null,
    init() { this.restart(); },
    restart() {
      clearTimeout(this.timer);
      if (count < 2 || matchMedia('(prefers-reduced-motion: reduce)').matches) return;
      this.timer = setTimeout(() => this.move(1), (dwells[this.index] || 6) * 1000);
    },
    move(step) {
      this.index = (this.index + step + count) % count;
      this.restart();
    },
    destroy() { clearTimeout(this.timer); },
  }));

  // ── search launch hints (_SearchLaunch) ──────────────────────────────────
  Alpine.data('hints', (hints) => ({
    i: 0,
    get label() {
      return hints.length ? `Search "${hints[this.i % hints.length]}"` : 'Search products, shops and more';
    },
    init() {
      if (hints.length > 1 && !matchMedia('(prefers-reduced-motion: reduce)').matches) {
        setInterval(() => this.i++, 4000);
      }
    },
  }));

  // ── product hero (_Hero) ─────────────────────────────────────────────────
  // A swipeable row of photos; the counter and arrows follow the scroll.
  Alpine.data('hero', (count) => ({
    count,
    page: 0,
    onScroll() {
      const el = this.$refs.track;
      this.page = Math.round(el.scrollLeft / el.clientWidth);
    },
    go(step) {
      const el = this.$refs.track;
      const reduced = matchMedia('(prefers-reduced-motion: reduce)').matches;
      el.scrollTo({ left: (this.page + step) * el.clientWidth, behavior: reduced ? 'auto' : 'smooth' });
    },
  }));

  // ── product quantity (DetailsScreen._qty) ────────────────────────────────
  // cap is the shop's stock (null when untracked). Totals per quantity are
  // formatted by the server, so the page never re-implements money text.
  Alpine.data('buy', (cap, totals, unit) => ({
    qty: 1,
    unit,
    get atCap() { return cap !== null && this.qty >= cap; },
    dec() { if (this.qty > 1) this.qty--; },
    inc() { if (!this.atCap && this.qty < 99) this.qty++; },
    t(key) {
      const list = totals[key];
      return list[Math.min(this.qty, list.length) - 1];
    },
  }));

  // ── sign-in card (LoginScreen) ───────────────────────────────────────────
  // One field per step; the button only wakes for something that could be
  // right, and the line under it says why it will not.
  Alpine.data('login', (step, email, error) => ({
    value: step === 'email' ? email : '',
    busy: false,
    error,
    get valid() {
      const v = this.value.trim();
      if (step === 'email') return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(v);
      if (step === 'code') return /^\d{6}$/.test(v);
      return this.value.length > 0;
    },
    get helper() {
      if (this.error) return this.error;
      if (this.value.trim() && !this.valid) {
        return {
          email: 'That is not an email address yet.',
          code: 'Enter the six digits from your email.',
          password: 'Enter your password to sign in.',
        }[step];
      }
      return {
        email: 'We only use your email for order updates and receipts.',
        code: `We sent a code to ${email}. It expires in 10 minutes.`,
        password: `Signing in as ${email}.`,
      }[step];
    },
  }));

  // ── seller: store form (SellerOnboardingScreen) ──────────────────────────
  Alpine.data('sellerStoreForm', (cfg) => ({
    ...cfg,
    picked: [...cfg.categories],
    file: null,
    preview: '',
    saving: false,
    get serviceable() { return this.cities.includes(this.city.trim()); },
    get blocker() {
      if (!this.name.trim()) return 'Add your business name';
      if (!this.location.trim()) return 'Add your store location';
      if (!this.serviceable) return `We only deliver around ${this.cities[0] || 'campus'}`;
      if (!this.picked.length) return 'Pick at least one category';
      return null;
    },
    toggle(c) {
      const i = this.picked.indexOf(c);
      if (i < 0) this.picked.push(c); else this.picked.splice(i, 1);
    },
    choose(e) {
      const f = e.target.files[0];
      e.target.value = '';
      if (!f) return;
      this.file = f;
      this.preview = URL.createObjectURL(f);
    },
    clearPhoto() { this.file = null; this.preview = ''; },
    async save() {
      if (this.blocker || this.saving) return;
      this.saving = true;
      const body = new FormData();
      body.set('name', this.name.trim());
      body.set('location', this.location.trim());
      body.set('city', this.city.trim());
      body.set('categories', this.picked.join(','));
      if (this.file) body.append('file', this.file);
      try {
        await sellerCall('POST', '/api/seller/store', body);
        location.assign('/seller');
      } catch (err) {
        lwToast(err.message, 'error');
        this.saving = false;
      }
    },
  }));

  // ── admin panel (AdminScreen) ────────────────────────────────────────────
  // One dialog, many decisions: `dlg` is its content, `dlg.run` its action.
  Alpine.data('adminPanel', (cfg) => ({
    ...cfg,
    dlg: { kind: '' },
    uploadTarget: '',
    open(d) {
      this.dlg = { cancel: 'Cancel', busy: false, ...d };
      this.$nextTick(() => this.$refs.dlg.showModal());
    },
    async call(method, path, body, message) {
      this.dlg.busy = true;
      try {
        const out = await sellerCall(method, `/staff-api/admin${path}`, body);
        return out ?? {};
      } catch (err) {
        this.$refs.dlg.close();
        lwToast(err.message, 'error');
        return null;
      } finally {
        this.dlg.busy = false;
      }
    },
    async done(method, path, body, message) {
      if (await this.call(method, path, body) !== null) lwReload(message);
    },
    confirm(title, body, action, run, danger = false, cancel = 'Cancel') {
      this.open({ kind: 'confirm', title, body, action, run, danger, cancel });
    },
    approve(owner, name) {
      this.confirm('Publish this store?', `${name || owner} will be visible to shoppers and able to list products.`,
        'Approve store', () => this.done('POST', `/stores/${encodeURIComponent(owner)}/approve`));
    },
    openReject(owner) {
      this.open({
        kind: 'reason', title: 'Why is this store not approved?', hint: 'The seller sees this, so make it actionable',
        value: '', action: 'Reject', danger: true, ready: () => this.dlg.value.trim() !== '',
        run: () => this.done('POST', `/stores/${encodeURIComponent(owner)}/reject`, { reason: this.dlg.value.trim() }),
      });
    },
    openDepts(owner, name, picked) {
      this.open({
        kind: 'depts', title: `${name} sells in`, picked: [...picked], action: 'Save',
        ready: () => this.dlg.picked.length > 0,
        run: () => this.done('PATCH', `/stores/${encodeURIComponent(owner)}/categories`, { categories: this.dlg.picked }),
      });
    },
    pickDept(name) {
      const p = this.dlg.picked;
      if (!p.includes(name)) p.push(name);
      else if (p[0] === name) p.splice(0, 1);
      else { p.splice(p.indexOf(name), 1); p.unshift(name); }
    },
    openAssign(id) {
      if (!this.activeRiders.length) { lwToast('Add a delivery number first.'); return; }
      this.open({ kind: 'assign', title: `Who takes #${id.toUpperCase()}?`, orderId: id, cancel: '' });
    },
    assign(phone) { this.done('POST', `/orders/${this.dlg.orderId}/assign`, { phone }); },
    openStock(item) {
      this.open({
        kind: 'stock', title: `Stock for ${item.title}`, value: String(item.stock), action: 'Save',
        body: `${item.reserved} of the current ${item.stock} ${item.reserved === 1 ? 'unit is' : 'units are'} already in live orders. Shoppers can buy ${item.available}.`,
        ready: () => /^\d+$/.test(String(this.dlg.value).trim()),
        run: () => {
          const next = Number(String(this.dlg.value).trim());
          if (next === item.stock) { this.$refs.dlg.close(); return; }
          this.done('PATCH', `/items/${item.id}/stock`, { stock: next }, `Stock for "${item.title}" set to ${next}.`);
        },
      });
    },
    async toggleListing(item) {
      if (await this.call('PATCH', `/items/${item.id}/listing`, { delisted: !item.delisted }) !== null) {
        lwReload(item.delisted ? `"${item.title}" is back on sale.` : `"${item.title}" is hidden from the shop.`);
      }
    },
    openItemDelete(item) {
      const blocked = item.orders > 0;
      this.confirm(
        blocked ? 'This product cannot be deleted' : 'Delete this product?',
        blocked
          ? `"${item.title}" has ${item.orders === 1 ? '1 order' : `${item.orders} orders`} against it. Deleting it would destroy those records, so the server refuses. Hide it from the shop instead — it stops selling and its history stays.`
          : `"${item.title}" from ${item.storeName} is removed for good, along with its photos. This cannot be undone.`,
        blocked ? 'Hide it instead' : 'Delete',
        () => blocked
          ? this.done('PATCH', `/items/${item.id}/listing`, { delisted: true }, `"${item.title}" is hidden from the shop.`)
          : this.done('DELETE', `/items/${item.id}`, undefined, `"${item.title}" deleted.`),
        !blocked, blocked ? 'Close' : 'Keep it');
    },
    openCategory(parent) {
      this.open({
        kind: 'category', title: parent ? `New category in ${parent}` : 'New department', parent,
        value: '', file: null, preview: '', icon: this.departmentIcons[0].key, colour: this.palette[0], action: 'Add',
        ready: () => this.dlg.value.trim() !== '',
        run: async () => {
          const name = this.dlg.value.trim();
          const made = await this.call('POST', '/categories', {
            name, parent, icon: parent ? '' : this.dlg.icon, colour: parent ? '' : this.dlg.colour,
          });
          if (made === null) return;
          if (this.dlg.file) {
            const body = new FormData();
            body.append('file', this.dlg.file);
            if (await this.call('POST', `/categories/${encodeURIComponent(name)}/photo`, body) === null) return;
          }
          lwReload();
        },
      });
    },
    pickCategoryPhoto(name) { this.uploadTarget = name; this.$refs.categoryPhoto.click(); },
    async uploadCategoryPhoto(e) {
      const file = e.target.files[0];
      e.target.value = '';
      if (!file) return;
      const body = new FormData();
      body.append('file', file);
      try {
        await sellerCall('POST', `/staff-api/admin/categories/${encodeURIComponent(this.uploadTarget)}/photo`, body);
        lwReload();
      } catch (err) { lwToast(err.message, 'error'); }
    },
    openCategoryDelete(name) {
      this.confirm(`Remove ${name}?`, 'It disappears from the shop and sellers can no longer list under it. Anything already filed under it has to be moved first.',
        'Remove', () => this.done('DELETE', `/categories/${encodeURIComponent(name)}`), true);
    },
    openGroup(group) {
      this.open({
        kind: 'group', title: group ? group.name : 'New group', isNew: !group, value: '', action: 'Save',
        fields: (group?.attributes || []).map((a) => ({ name: a.name, unit: a.unit, mode: a.mode || '', perUnit: !!a.perUnit })),
        run: () => {
          const name = group ? group.name : this.dlg.value.trim();
          if (!name) { this.$refs.dlg.close(); return; }
          const attributes = this.dlg.fields.map((f) => ({
            name: f.name, unit: f.unit, ...(f.mode ? { mode: f.mode } : {}), ...(f.perUnit ? { perUnit: true } : {}),
          }));
          this.done('POST', '/compare-groups', { name, attributes });
        },
      });
    },
    deleteGroup(name) { this.done('DELETE', `/compare-groups/${encodeURIComponent(name)}`); },
    showPin(pin, phone) {
      this.open({
        kind: 'pin', title: 'Give them this PIN', cancel: '', action: 'Done', run: () => lwReload(),
        body: `${pin}\n\nThey sign in at /delivery with ${phone} and this PIN. It is shown once — issuing another replaces it.`,
      });
    },
    openRiderAdd() {
      this.open({
        kind: 'rider', title: 'Add a delivery number', value: '', name: '', action: 'Add',
        run: async () => {
          const out = await this.call('POST', '/riders', { phone: this.dlg.value, name: this.dlg.name });
          if (out !== null) this.showPin(out.pin || '', this.dlg.value.trim());
        },
      });
    },
    issuePin(phone) {
      this.confirm(`New PIN for ${phone}?`, 'Their old PIN stops working straight away, and they are signed out of the delivery panel until they use the new one.',
        'Issue PIN', async () => {
          const out = await this.call('POST', `/riders/${phone}/pin`);
          if (out !== null) this.showPin(out.pin || '', phone);
        });
    },
    openNumber(phone) {
      this.open({
        kind: 'number', title: `Move ${phone} to a new number`, value: '', action: 'Move',
        run: async () => {
          const next = this.dlg.value.trim();
          const out = await this.call('POST', `/riders/${phone}/number`, { phone: next });
          if (out !== null) this.showPin(out.pin || '', next);
        },
      });
    },
    switchRider(phone, active) {
      if (!active) { this.done('POST', `/riders/${phone}/on`); return; }
      this.confirm(`Switch off ${phone}?`, 'They stop being handed orders and are signed out of the delivery panel. Their deliveries stay on their name, and you can switch them back on with the same PIN.',
        'Switch off', () => this.done('DELETE', `/riders/${phone}`));
    },
    deleteRider(phone) {
      this.confirm(`Remove ${phone} for good?`, 'The rider is deleted. Anything they were assigned but had not collected goes back to the other riders. This cannot be undone — switch them off instead if they might come back.',
        'Remove', () => this.done('DELETE', `/riders/${phone}?forever=true`));
    },
    openBannerDelete(id, title) {
      this.confirm('Delete this banner?', `“${title}” will be removed from the storefront. You can hide it in Edit instead.`,
        'Delete', () => this.done('DELETE', `/campaigns/${encodeURIComponent(id)}`), false, 'Keep banner');
    },
  }));

  // ── admin: banner editor (CampaignEditor) ────────────────────────────────
  const presets = [
    { name: 'Forest / Lime', hex: '#143E32', background: '#143E32', foreground: '#FFFFFF', action: '#C6EE63', actionForeground: '#143E32' },
    { name: 'Teal / Reef', hex: '#0E3B45', background: '#0E3B45', foreground: '#FFFFFF', action: '#7FE3D4', actionForeground: '#0E3B45' },
    { name: 'Ink / Mist', hex: '#263244', background: '#263244', foreground: '#FFFFFF', action: '#C8DBEE', actionForeground: '#263244' },
    { name: 'Indigo / Iris', hex: '#2A2160', background: '#2A2160', foreground: '#FFFFFF', action: '#C3B6FF', actionForeground: '#2A2160' },
    { name: 'Berry / Blush', hex: '#5B1D3D', background: '#5B1D3D', foreground: '#FFFFFF', action: '#F7B7D2', actionForeground: '#5B1D3D' },
    { name: 'Marigold / Maroon', hex: '#7A1F12', background: '#7A1F12', foreground: '#FFFFFF', action: '#F2B441', actionForeground: '#4A1008' },
    { name: 'Saffron / Amber', hex: '#7A2E12', background: '#7A2E12', foreground: '#FFFFFF', action: '#FFC46B', actionForeground: '#4A1B08' },
    { name: 'Cacao / Peach', hex: '#4A3029', background: '#4A3029', foreground: '#FFFFFF', action: '#F7A38E', actionForeground: '#4A3029' },
  ];
  const rgb = (hex) => [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16));
  const hexOf = (c) => `#${c.map((v) => Math.round(v).toString(16).padStart(2, '0')).join('')}`.toUpperCase();
  const luminance = (hex) => {
    const [r, g, b] = rgb(hex).map((v) => { v /= 255; return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4; });
    return 0.2126 * r + 0.7152 * g + 0.0722 * b;
  };
  const contrast = (a, b) => { const x = luminance(a), y = luminance(b); return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05); };
  // CampaignPalette.resolve, without a season: presets, the legacy pastels,
  // then any valid hex made readable.
  const resolvePalette = (raw) => {
    const hex = String(raw || '').trim().toUpperCase();
    const byHex = (h) => presets.find((p) => p.hex === h);
    if (byHex(hex)) return byHex(hex);
    if (['#F2E8CE', '#DCEACD', '#1D4A3C'].includes(hex)) return presets[0];
    if (['#F7DCCB', '#E7DFF3'].includes(hex)) return presets[7];
    if (hex === '#DCE9F5') return presets[2];
    if (!/^#[0-9A-F]{6}$/.test(hex)) return presets[0];
    if (contrast('#17221D', hex) >= 4.5) return { background: hex, foreground: '#17221D', action: '#143E32', actionForeground: '#FFFFFF' };
    let bg = rgb(hex);
    for (let i = 0; i < 24 && contrast('#FFFFFF', hexOf(bg)) < 4.5; i++) bg = bg.map((v) => v * 0.92);
    return { background: hexOf(bg), foreground: '#FFFFFF', action: '#C6EE63', actionForeground: '#1D4939' };
  };

  Alpine.data('bannerEditor', (cfg) => ({
    ...cfg,
    presets,
    preview: cfg.imageUrl,
    busy: false,
    error: '',
    settle: null,
    init() {
      this.$watch('imageUrl', (v) => {
        clearTimeout(this.settle);
        this.settle = setTimeout(() => { this.preview = v.trim(); }, 350);
      });
    },
    get palette() { return resolvePalette(this.colour); },
    get wash() {
      const bg = this.palette.background;
      const [r, g, b] = rgb(bg);
      const a = (x) => `rgba(${r},${g},${b},${x})`;
      return `background-image:linear-gradient(to right, ${bg} 0%, ${a(0.98)} 48%, ${a(0.42)} 80%, ${a(0.04)} 100%)`;
    },
    lighten(hex) { return hexOf(rgb(hex).map((v) => v * 0.64 + 255 * 0.36)); },
    isVideo(url) { return url.includes('/video/upload/') || /\.(mp4|webm|mov|m4v)(\?|$)/i.test(url); },
    get detected() {
      const url = this.preview;
      if (!url) return { text: 'Paste a link to a photo, GIF or clip — or a Pinterest, Instagram or blog page, then press Fetch and we will pull the picture out of it.', wrong: false };
      let parsed = null;
      try { parsed = new URL(url); } catch {}
      if (!parsed || parsed.protocol !== 'https:' || !parsed.host) return { text: 'Use a full https:// link.', wrong: true };
      if (this.isVideo(url)) return { text: 'Clip — plays muted, loops, no sound.', wrong: false };
      if (/\.gif(\?|$)/i.test(url)) return { text: 'Animation — delivered as a clip, not as a GIF.', wrong: false };
      return { text: 'Photo. If this is a page rather than a file, press Fetch.', wrong: false };
    },
    async fetchLink() {
      this.busy = true; this.error = '';
      try {
        const out = await sellerCall('POST', '/staff-api/admin/campaign-media', { url: this.imageUrl.trim() });
        this.imageUrl = out.imageUrl; this.preview = out.imageUrl;
      } catch (err) { this.error = err.message; }
      this.busy = false;
    },
    async upload(e) {
      const file = e.target.files[0];
      e.target.value = '';
      if (!file) return;
      this.busy = true; this.error = '';
      const body = new FormData();
      body.append('file', file, file.name);
      try {
        const out = await sellerCall('POST', '/staff-api/admin/campaign-photos', body);
        this.imageUrl = out.imageUrl; this.preview = out.imageUrl;
      } catch { this.error = 'Artwork upload failed. Your other edits are still here; try again.'; }
      this.busy = false;
    },
    validate() {
      if (!this.title.trim()) return 'Add a headline';
      if (!this.cta.trim()) return 'Name the button action';
      const art = this.imageUrl.trim();
      if (art) {
        let u = null;
        try { u = new URL(art); } catch {}
        if (!u || u.protocol !== 'https:' || !u.host || u.username) return 'Use a valid HTTPS URL';
      }
      if (!/^#[0-9a-fA-F]{6}$/.test(this.colour.trim())) return 'Use a hex colour such as #143E32';
      const n = Number(this.position);
      if (!/^\d+$/.test(String(this.position).trim()) || n > 9999) return 'Enter a number from 0 to 9999';
      return '';
    },
    async save() {
      this.error = this.validate();
      if (this.error) return;
      this.busy = true;
      const id = this.id || `banner-${Date.now()}000`;
      try {
        await sellerCall('PUT', `/staff-api/admin/campaigns/${encodeURIComponent(id)}`, {
          id, title: this.title.trim(), subtitle: this.subtitle.trim(), cta: this.cta.trim(),
          category: this.category, department: this.department, imageUrl: this.imageUrl.trim(),
          colour: this.colour.trim(), enabled: this.enabled, position: Number(this.position),
        });
        location.assign('/admin?tab=banners');
      } catch (err) {
        this.error = `Could not save banner. ${err.message}`;
        this.busy = false;
      }
    },
  }));

  // ── admin: policy editor (_PolicyEditor) ─────────────────────────────────
  Alpine.data('policyEditor', (cfg) => ({
    ...cfg,
    preview: false,
    busy: false,
    error: '',
    // PolicyBody, as the shop draws it: blank lines split blocks, a "## " line
    // is a heading, hard-wrapped lines join unless the next is a list item.
    get blocks() {
      const out = [];
      for (const raw of this.body.replace(/\r\n/g, '\n').split(/\n\s*\n/)) {
        let block = raw.trim();
        if (!block) continue;
        if (block.startsWith('## ')) {
          const nl = block.indexOf('\n');
          out.push({ heading: true, text: (nl < 0 ? block : block.slice(0, nl)).slice(3).trim() });
          if (nl < 0) continue;
          block = block.slice(nl + 1).trim();
          if (!block) continue;
        }
        out.push({ heading: false, text: block.replace(/\n(?!-)/g, ' ') });
      }
      return out;
    },
    async save() {
      this.busy = true; this.error = '';
      try {
        await sellerCall('PUT', `/staff-api/admin/policies/${encodeURIComponent(this.slug)}`, { title: this.title.trim(), body: this.body });
        location.assign('/admin?tab=policies');
      } catch (err) {
        this.error = err.message;
        this.busy = false;
      }
    },
  }));

  // ── admin: one product's photos (AdminPhotosScreen) ──────────────────────
  Alpine.data('adminItemPhotos', (id, urls) => ({
    id,
    shots: urls.map((url) => ({ url })),
    busy: false,
    ...photoMethods('/staff-api/admin/items'),
  }));

  // ── seller: product form (SellerProductScreen) ───────────────────────────
  Alpine.data('sellerProductForm', (cfg) => ({
    ...cfg,
    price: cfg.price ? String(cfg.price) : '',
    mrp: cfg.mrp > 0 ? String(cfg.mrp) : '',
    stock: cfg.id ? String(cfg.stock) : '',
    shots: cfg.imageUrls.map((url) => ({ url })),
    options: cfg.options.map((o) => ({ name: o.name, kind: o.kind || '', values: [...(o.values || [])], entry: '' })),
    attrs: { ...cfg.attributes },
    section: '',
    busy: false,
    saving: false,
    init() {
      if (!this.category && this.sections.length) this.category = this.sections[0].leaves[0];
      this.section = this.sectionOf(this.category) || (this.sections[0] ? this.sections[0].name : '');
    },
    sectionOf(c) {
      const s = this.sections.find((s) => s.leaves.includes(c));
      return s ? s.name : '';
    },
    get leaves() {
      const s = this.sections.find((s) => s.name === this.section);
      return s ? s.leaves : [];
    },
    pickSection(name) { this.section = name; this.category = this.leaves[0]; },
    get priceValue() { const t = this.price.trim(); return t === '' ? NaN : Number(t); },
    get mrpValue() { const t = this.mrp.trim(); return t === '' ? 0 : Number(t); },
    get stockValue() { const t = this.stock.trim(); return /^\d+$/.test(t) ? Number(t) : NaN; },
    get blocker() {
      if (!this.shots.length) return 'Add at least one photo';
      if (!this.title.trim()) return 'Give the product a title';
      if (!(this.priceValue > 0)) return 'Set a price above ₹0';
      if (Number.isNaN(this.mrpValue)) return 'MRP must be a number, or left blank';
      if (this.mrpValue > 0 && this.mrpValue < this.priceValue) return 'MRP cannot be below the selling price';
      if (!(this.stockValue >= 0)) return 'Enter how many units you have';
      return null;
    },
    get discount() {
      const mrp = this.mrpValue || 0, price = this.priceValue || 0;
      if (mrp <= 0 || price <= 0) return null;
      if (mrp < price) return { bad: true, text: 'MRP is below your selling price — buyers would see a markup, not a discount.' };
      if (mrp === price) return { bad: false, text: 'Same as the selling price, so no discount is shown.' };
      const off = Math.round(((mrp - price) / mrp) * 100);
      const saved = (mrp - price).toLocaleString('en-IN', { maximumFractionDigits: 2 });
      return { bad: false, text: `Buyers see ${off}% OFF — ₹${saved} saved.` };
    },
    get presetList() { return this.presets[this.deptOf[this.category] || ''] || this.anyPresets; },
    get unusedPresets() { return this.presetList.filter((p) => !this.options.some((o) => o.name === p.name)); },
    addPreset(p) { this.options.push({ name: p.name, kind: p.kind || '', values: [...p.values], entry: '' }); },
    addCustom() {
      const name = (window.prompt('What do buyers choose? e.g. Flavour, Length, Material') || '').trim();
      if (name) this.options.push({ name, kind: '', values: [], entry: '' });
    },
    addValue(o, v) {
      v = String(v || '').trim();
      if (v && !o.values.includes(v)) o.values.push(v);
      o.entry = '';
    },
    swatchName(hex) {
      const s = this.swatches.find((s) => s.hex.toLowerCase() === String(hex).toLowerCase());
      return s ? s.name : hex;
    },
    get template() {
      const g = this.groups.find((g) => g.name === this.compareGroup);
      return g ? g.attributes : [];
    },
    get unwinnable() {
      return this.template
        .filter((f) => (f.mode === 'higher_better' || f.mode === 'lower_better') && !String(this.attrs[f.name] || '').trim())
        .map((f) => f.name);
    },
    liveOptions() {
      return this.options.filter((o) => o.values.length)
        .map((o) => (o.kind ? { name: o.name, kind: o.kind, values: o.values } : { name: o.name, values: o.values }));
    },
    liveAttrs() {
      const names = new Set(this.template.map((f) => f.name));
      const out = {};
      for (const [k, v] of Object.entries(this.attrs)) {
        if (names.has(k) && String(v || '').trim()) out[k] = String(v).trim();
      }
      return out;
    },
    ...photoMethods('/api/seller/items'),
    async save() {
      if (this.blocker || this.saving) return;
      this.saving = true;
      const fields = {
        title: this.title.trim(), description: this.description.trim(), category: this.category,
        price: this.priceValue, mrp: this.mrpValue, compareGroup: this.compareGroup, stock: this.stockValue,
      };
      try {
        if (this.id) {
          await sellerCall('PATCH', `/api/seller/items/${this.id}`, { ...fields, options: this.liveOptions(), attributes: this.liveAttrs() });
        } else {
          const body = new FormData();
          for (const [k, v] of Object.entries(fields)) body.set(k, v);
          body.set('options', JSON.stringify(this.liveOptions()));
          body.set('attributes', JSON.stringify(this.liveAttrs()));
          this.shots.forEach((s) => s.file && body.append('file', s.file));
          await sellerCall('POST', '/api/seller/items', body);
        }
        location.assign('/seller?pane=inventory');
      } catch (err) {
        lwToast(err.message, 'error');
        this.saving = false;
      }
    },
  }));

  // ── seller: dashboard actions (SellerDashboardScreen) ────────────────────
  Alpine.data('sellerDashboard', (pane) => ({
    pane,
    shownOrders: 8,
    shownItems: 8,
    rejecting: null,
    reason: '',
    removing: null,
    init() {
      this.$watch('pane', (v) => history.replaceState(null, '', `?pane=${v}`));
    },
    async act(method, path, body, message) {
      try {
        await sellerCall(method, path, body);
        lwReload(message);
      } catch (err) {
        lwToast(err.message, 'error');
      }
    },
    accept(id) {
      this.act('POST', `/api/seller/orders/${id}/accept`, {}, 'Accepted. The customer has their delivery code — a rider takes it from here.');
    },
    openReject(id) { this.rejecting = id; this.reason = ''; this.$refs.reject.showModal(); },
    reject() {
      const reason = this.reason.trim();
      if (!reason) return;
      this.$refs.reject.close();
      this.act('POST', `/api/seller/orders/${this.rejecting}/reject`, { reason });
    },
    stock(id, delta) { this.act('PATCH', `/api/seller/items/${id}/stock`, { delta }); },
    listing(id, title, hide) {
      this.act('PATCH', `/api/seller/items/${id}/listing`, { delisted: hide },
        hide ? `"${title}" is hidden from the shop.` : `"${title}" is back on sale.`);
    },
    openRemove(id, title) { this.removing = { id, title }; this.$refs.remove.showModal(); },
    remove() {
      const r = this.removing;
      this.$refs.remove.close();
      this.act('DELETE', `/api/seller/items/${r.id}`);
    },
  }));

  // ── gallery component ────────────────────────────────────────────────────
  // Used by templates/components/product/gallery.templ.
  Alpine.data('gallery', () => ({
    selected: 0,
    total: 0,

    init() {
      // Count the images by looking at siblings rendered by the templ.
      this.total = this.$el.querySelectorAll('img[x-show]').length;
    },

    select(i) {
      this.selected = i;
    },

    prev() {
      this.selected = (this.selected - 1 + this.total) % this.total;
    },

    next() {
      this.selected = (this.selected + 1) % this.total;
    },
  }));

  // ── search component ─────────────────────────────────────────────────────
  // Used by templates/pages/search.templ (if present) and the search bar.
  Alpine.data('search', () => ({
    query: '',
    focused: false,

    init() {
      // Pre-fill from the URL on the search page.
      const q = new URLSearchParams(window.location.search).get('q');
      if (q) this.query = q;
    },

    clear() {
      this.query = '';
      this.$refs.input?.focus();
    },
  }));

  // ── modal component ──────────────────────────────────────────────────────
  // Used by any template that needs a full-screen confirm dialog.
  Alpine.data('modal', (options = {}) => ({
    open: false,
    title: options.title || '',
    message: options.message || '',
    confirmLabel: options.confirmLabel || 'Confirm',
    cancelLabel: options.cancelLabel || 'Cancel',
    onConfirm: options.onConfirm || null,

    show({ title, message, confirmLabel, cancelLabel, onConfirm } = {}) {
      this.title = title || this.title;
      this.message = message || this.message;
      this.confirmLabel = confirmLabel || this.confirmLabel;
      this.cancelLabel = cancelLabel || this.cancelLabel;
      this.onConfirm = onConfirm || this.onConfirm;
      this.open = true;
    },

    confirm() {
      if (this.onConfirm) this.onConfirm();
      this.open = false;
    },

    cancel() {
      this.open = false;
    },
  }));

}); // end alpine:init

// ── Seller API helpers ──────────────────────────────────────────────────
// The seller pages call the backend's own /api/seller routes through the
// site's proxy, which adds the session token. Errors come back as
// {"error": "..."} and are thrown with that sentence.
window.sellerCall = async (method, path, body) => {
  const opts = { method, headers: {} };
  if (body instanceof FormData) {
    opts.body = body;
  } else if (body !== undefined) {
    opts.headers['Content-Type'] = 'application/json';
    opts.body = JSON.stringify(body);
  }
  let res;
  try {
    res = await fetch(path, opts);
  } catch {
    throw new Error('Could not reach Uniminute. Check your connection and try again.');
  }
  if (res.ok) return res.status === 204 ? null : res.json().catch(() => null);
  // Same fallbacks as statusMessage in handlers.go.
  let message = res.status === 401 ? 'Your session ended. Sign in again.'
    : res.status === 403 ? "You don't have access to do that."
    : res.status === 404 ? 'That is no longer here. Refresh the page and try again.'
    : res.status === 413 ? 'That file is too large. Choose a smaller one.'
    : res.status === 429 ? 'Too many attempts. Wait a minute, then try again.'
    : res.status >= 500 ? 'Uniminute had a problem on its side. Try again in a moment.'
    : "That didn't go through. Check the details and try again.";
  try { message = (await res.json()).error || message; } catch {}
  throw new Error(message);
};

// PhotoManager's behaviour, for any component with `id`, `shots` and `busy`.
// A saved listing's photos change the moment they are changed: upload the new
// ones, then send the whole order as URLs the server already knows. base is
// the items route (the seller's own, or the admin's through its proxy).
window.photoMethods = (base) => ({
  addFiles(e) {
    const files = [...e.target.files];
    e.target.value = '';
    if (files.length) this.setShots([...this.shots, ...files.map((file) => ({ file, preview: URL.createObjectURL(file) }))]);
  },
  remove(i) { const s = [...this.shots]; s.splice(i, 1); this.setShots(s); },
  move(i, step) {
    const j = i + step;
    if (j < 0 || j >= this.shots.length) return;
    const s = [...this.shots];
    [s[i], s[j]] = [s[j], s[i]];
    this.setShots(s);
  },
  async setShots(next) {
    this.shots = next;
    if (!this.id) return;
    this.busy = true;
    try {
      let urls = next.filter((s) => s.url).map((s) => s.url);
      const fresh = next.filter((s) => s.file);
      // One upload per photo, so one bad file doesn't lose the others.
      const failed = [];
      let lastError = '';
      for (const shot of fresh) {
        const body = new FormData();
        body.append('file', shot.file);
        try {
          const after = await sellerCall('POST', `${base}/${this.id}/photos`, body);
          urls = [...urls, ...after.imageUrls.filter((u) => !urls.includes(u))];
        } catch (err) {
          failed.push(shot);
          lastError = err.message;
        }
      }
      const saved = await sellerCall('PUT', `${base}/${this.id}/photos`, { imageUrls: urls });
      this.shots = saved.imageUrls.map((url) => ({ url }));
      if (fresh.length > 1 && failed.length && failed.length < fresh.length) {
        lwToast(`${fresh.length - failed.length} of ${fresh.length} photos added. ${failed.length} failed: ${lastError}`, 'error');
      } else if (failed.length) {
        lwToast(lastError, 'error');
      } else if (fresh.length) {
        lwToast(fresh.length === 1 ? 'Photo added.' : `${fresh.length} photos added.`, 'success');
      }
    } catch (err) {
      lwToast(err.message, 'error');
    }
    this.busy = false;
  },
});

window.lwToast = (message, tone) => window.dispatchEvent(new CustomEvent('lw:toast', { detail: { message, tone } }));

// Reloads the page and shows message on the fresh one.
window.lwReload = (message) => {
  if (message) {
    try { sessionStorage.setItem('lw:flash', message); } catch {}
  }
  location.reload();
};

window.addEventListener('alpine:initialized', () => {
  let flash = null;
  try {
    flash = sessionStorage.getItem('lw:flash');
    sessionStorage.removeItem('lw:flash');
  } catch {}
  if (flash) setTimeout(() => lwToast(flash, 'success'), 50);
});

// ── HTMX event hooks ─────────────────────────────────────────────────────
// HTMX fires every HX-Trigger event on the requesting element and it bubbles
// to window, so listeners there need no re-dispatch.

// Update the cart badges (bottom bar and top bar) whenever the server tells us to.
// The server includes HX-Trigger: {"lw:cart:count": {"count": N}}.
window.addEventListener('lw:cart:count', (e) => {
  const count = e.detail?.count;
  document.querySelectorAll('[data-cart-count]').forEach((badge) => {
    const shown = typeof count === 'number' && count > 0;
    if (shown) badge.textContent = count;
    badge.hidden = !shown;
  });
});

// Close any open drawer when the back button is pressed.
window.addEventListener('popstate', () => {
  if (window.Alpine) {
    Alpine.store('drawers').closeAll();
  }
});

// ── App states: offline, loading, confirmation, processing ───────────────
// Markup lives in ui.templ AppStates, mounted once in the base layout.

document.addEventListener('alpine:init', () => {
  Alpine.data('netStatus', () => ({
    online: navigator.onLine,
    init() {
      window.addEventListener('online', () => { this.online = true; lwToast("You're back online.", 'success'); });
      window.addEventListener('offline', () => { this.online = false; });
    },
  }));

  Alpine.data('confirmDialog', () => ({
    title: '', body: '', ok: 'Confirm', cancel: 'Cancel', danger: false, resolve: null,
    init() {
      window.lwConfirm = (opts) => new Promise((resolve) => {
        this.settle(false);
        Object.assign(this, { title: '', body: '', ok: 'Confirm', cancel: 'Cancel', danger: false }, opts, { resolve });
        this.$el.showModal();
      });
    },
    settle(answer) {
      const done = this.resolve;
      this.resolve = null;
      if (this.$el.open) this.$el.close();
      if (done) done(answer);
    },
  }));

  Alpine.data('processing', () => ({
    active: false, title: '',
    init() {
      const guard = (e) => { e.preventDefault(); e.returnValue = ''; };
      window.lwProcessing = (title) => {
        this.active = !!title;
        this.title = title || '';
        if (title) window.addEventListener('beforeunload', guard);
        else window.removeEventListener('beforeunload', guard);
      };
      // The server has answered: let its redirect through, keep the overlay up.
      window.lwProcessingRelease = () => window.removeEventListener('beforeunload', guard);
    },
  }));
});

// hx-confirm="Title\n\nBody" opens the styled dialog instead of window.confirm.
document.addEventListener('htmx:confirm', (e) => {
  const question = e.detail.question;
  if (!question || !window.lwConfirm) return;
  e.preventDefault();
  const [title, ...rest] = question.split('\n\n');
  const el = e.detail.elt;
  lwConfirm({
    title,
    body: rest.join('\n\n'),
    ok: el.dataset.confirmOk || 'Confirm',
    cancel: el.dataset.confirmCancel || 'Cancel',
    danger: el.dataset.confirmDanger !== undefined,
  }).then((yes) => { if (yes) e.detail.issueRequest(true); });
});

// Loading: a thin bar while any HTMX request or page navigation is running.
(() => {
  let inFlight = 0, timer = null;
  const bar = () => document.getElementById('lw-progress');
  const show = () => {
    const el = bar(); if (!el) return;
    clearTimeout(timer);
    el.style.opacity = '1';
    el.style.transform = 'scaleX(0.7)';
  };
  const hide = () => {
    const el = bar(); if (!el) return;
    el.style.transform = 'scaleX(1)';
    timer = setTimeout(() => { el.style.opacity = '0'; el.style.transform = 'scaleX(0)'; }, 250);
  };
  document.addEventListener('htmx:beforeRequest', () => { inFlight++; show(); });
  document.addEventListener('htmx:afterRequest', () => { inFlight = Math.max(0, inFlight - 1); if (!inFlight) hide(); });
  window.addEventListener('beforeunload', show);
  window.addEventListener('pageshow', hide);
})();

// Error and offline: HTMX requests that never reached the server, or failed.
document.addEventListener('htmx:sendError', () => {
  lwToast(navigator.onLine ? 'Could not reach Uniminute. Try again in a moment.' : "You're offline. Reconnect and try again.", 'error');
});
document.addEventListener('htmx:responseError', (e) => {
  const status = e.detail.xhr.status;
  if (status === 401) {
    location.href = '/login?expired=1&next=' + encodeURIComponent(location.pathname + location.search);
    return;
  }
  lwToast(status === 403 ? "You don't have access to do that."
    : status === 404 ? 'That is no longer here. Refresh the page and try again.'
    : status === 429 ? 'Too many attempts. Wait a minute, then try again.'
    : status >= 500 ? 'Uniminute had a problem on its side. Try again in a moment.'
    : "That didn't go through. Try again.", 'error');
});

// Permission required: the browser's notification prompt, asked for on purpose.
document.addEventListener('alpine:init', () => {
  Alpine.data('notifyPermission', () => ({
    state: 'Notification' in window ? Notification.permission : 'unsupported',
    async ask() {
      this.state = await Notification.requestPermission();
      if (this.state === 'granted') lwToast('Notifications are on.', 'success');
    },
  }));
});

// Admin: checkout charges (Delivery plus any extras). Mirrors validCharges in backend/charges.go.
document.addEventListener('alpine:init', () => {
  Alpine.data('chargesEditor', (initial) => ({
    rows: (initial || []).map((c, i) => ({ ...c, key: i })),
    saving: false,
    add() { this.rows.push({ id: '', name: '', amount: 0, key: Date.now() }); },
    total() {
      const t = this.rows.reduce((s, c) => s + (Number(c.amount) || 0), 0);
      return Math.round(t * 100) / 100;
    },
    problem() {
      const seen = new Set();
      for (const c of this.rows) {
        const name = (c.name || '').trim();
        if (!name) return 'Every charge needs a name.';
        if (seen.has(name.toLowerCase())) return `Two charges are both called ${name}.`;
        seen.add(name.toLowerCase());
        if (!(c.amount >= 0 && c.amount <= 10000)) return `${name}: enter an amount from ₹0 to ₹10,000.`;
      }
      return '';
    },
    async save() {
      if (this.saving || this.problem()) return;
      this.saving = true;
      try {
        await sellerCall('PUT', '/staff-api/admin/charges',
          this.rows.map(({ id, name, amount }) => ({ id, name: name.trim(), amount: Number(amount) })));
        lwReload('Charges saved. The next order uses them.');
      } catch (err) {
        lwToast(err.message, 'error');
        this.saving = false;
      }
    },
  }));
});
