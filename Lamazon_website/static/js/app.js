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

  // Storefront category navigation and the phone's native menu dialog.
  Alpine.data('storeMenu', (initial, sections, initialQuery = '') => {
    const visibleImages = new Set();
    let observer, slowTimer;
    return {
      active: initial,
      sections,
      query: initialQuery,
      menuOpen: false,
      loading: false,
      slow: false,
      offline: !navigator.onLine,
      matches(text) {
        const haystack = text.toLowerCase();
        return this.query.toLowerCase().trim().split(/\s+/).filter(Boolean).every((word) => haystack.includes(word));
      },
      sectionCount(name) {
        const section = this.sections.find((s) => s.name === name);
        return section ? section.items.filter((text) => this.matches(text)).length : 0;
      },
      showSection(name) {
        return (!this.active || this.active === name) && this.sectionCount(name) > 0;
      },
      get allCount() {
        return this.sections.reduce((total, s) => total + this.sectionCount(s.name), 0);
      },
      get count() {
        return this.sections.filter((s) => !this.active || s.name === this.active)
          .reduce((total, s) => total + this.sectionCount(s.name), 0);
      },
      search(value) {
        this.query = value;
        // A new search covers the whole shop, including categories off screen.
        if (value.trim()) this.active = '';
        this.updateURL(false);
        this.$nextTick(() => this.updateLoading());
      },
      updateURL(push) {
        const url = new URL(location.href);
        if (this.active) url.searchParams.set('category', this.active);
        else url.searchParams.delete('category');
        if (this.query.trim()) url.searchParams.set('q', this.query.trim());
        else url.searchParams.delete('q');
        if (push) url.hash = 'store-menu-results';
        history[push ? 'pushState' : 'replaceState'](null, '', url);
      },
      init() {
        this.updateLoading = () => {
          const pending = [...visibleImages].some((img) => !img.complete && img.getClientRects().length > 0);
          if (pending && !this.loading) {
            this.slow = false;
            slowTimer = setTimeout(() => { this.slow = true; }, 8000);
          } else if (!pending) {
            clearTimeout(slowTimer);
            this.slow = false;
          }
          this.loading = pending;
        };
        this.onConnection = () => { this.offline = !navigator.onLine; };
        this.$el.addEventListener('load', this.updateLoading, true);
        this.$el.addEventListener('error', this.updateLoading, true);
        window.addEventListener('online', this.onConnection);
        window.addEventListener('offline', this.onConnection);
        // Track only images near the viewport; lazy images farther down must not
        // keep the menu busy. Load and error both settle an image.
        observer = new IntersectionObserver((entries) => {
          entries.forEach(({ target, isIntersecting }) => {
            if (isIntersecting) visibleImages.add(target);
            else visibleImages.delete(target);
          });
          this.updateLoading();
        }, { rootMargin: '200px 0px' });
        this.$nextTick(() => {
          this.$refs.products?.querySelectorAll('img').forEach((img) => observer.observe(img));
        });
        this.onHistory = () => {
          const url = new URL(location.href);
          const category = url.searchParams.get('category') || '';
          this.query = url.searchParams.get('q') || '';
          this.active = this.sections.find((s) => s.name.toLowerCase() === category.trim().toLowerCase())?.name || '';
          if (this.$refs.menu?.open) this.$refs.menu.close();
        };
        window.addEventListener('popstate', this.onHistory);
      },
      destroy() {
        window.removeEventListener('popstate', this.onHistory);
        window.removeEventListener('online', this.onConnection);
        window.removeEventListener('offline', this.onConnection);
        this.$el.removeEventListener('load', this.updateLoading, true);
        this.$el.removeEventListener('error', this.updateLoading, true);
        observer?.disconnect();
        clearTimeout(slowTimer);
      },
      openMenu() { this.menuOpen = true; this.$refs.menu.showModal(); },
      choose(category) {
        this.active = category;
        this.updateURL(true);
        if (this.$refs.menu.open) this.$refs.menu.close();
        this.$nextTick(() => {
          this.$refs.results.focus({ preventScroll: true });
          this.$refs.results.scrollIntoView({ block: 'start', behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' });
        });
      },
    };
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
  Alpine.data('buy', (cap, basePrice, mrp, variants = [], optionNames = []) => ({
    qty: 1,
    picks: {},
    get variant() {
      if (!variants.length) return null;
      return variants.find((v) => v.choices.every((c) => this.picks[c.name] === c.value)) || null;
    },
    get selectedPrice() { return this.variant ? Number(this.variant.price) : basePrice; },
    // Each combination carries its own MRP; without one it falls back to the listing's.
    get selectedMRP() { return this.variant ? Number(this.variant.mrp || 0) : mrp; },
    get unit() { return this.selectedPrice.toLocaleString('en-IN', { maximumFractionDigits: 2 }); },
    get discountPercent() { return this.selectedMRP > this.selectedPrice ? Math.round((this.selectedMRP - this.selectedPrice) / this.selectedMRP * 100) : 0; },
    // The first option still to choose, or '' when the buyer can add to cart.
    get missing() { return optionNames.find((n) => !this.picks[n]) || ''; },
    get atCap() { return cap !== null && this.qty >= cap; },
    dec() { if (this.qty > 1) this.qty--; },
    inc() { if (!this.atCap && this.qty < 99) this.qty++; },
    t(key) {
      const amount = key === 'mrp' ? this.selectedMRP : key === 'save' ? this.selectedMRP - this.selectedPrice : this.selectedPrice;
      return (amount * this.qty).toLocaleString('en-IN', { maximumFractionDigits: 2 });
    },
  }));

  // ── Razorpay checkout ───────────────────────────────────────────────────
  // The server recalculates the cart total before creating the Razorpay order;
  // this amount is only an early stale-page check, never a trusted price.
  Alpine.data('razorpayCheckout', (amount) => {
    let activeCheckout = null;
    let attempt = 0;
    let cooldownTimer = null;
    return {
      amount,
      method: 'razorpay',
      busy: false,
      cashBusy: false,
      error: '',
      paymentFailed: false,
      gatewayOpen: false,
      amountLimited: false,
      pendingPayment: null,
      pendingAttempt: false,
      cooldown: 0,
      init() {
        try {
          const saved = JSON.parse(sessionStorage.getItem('uniminute-pending-payment') || 'null');
          if (saved && saved.amount === this.amount && saved.expires > Date.now()) {
            this.pendingPayment = saved.payment;
            this.error = 'Payment confirmation is pending. Retry order confirmation before making another payment.';
          }
        } catch {}
        ['paymentFailed', 'pendingPayment', 'gatewayOpen'].forEach((key) => {
          this.$watch(key, () => this.syncFailureDialog());
        });
      },
      destroy() {
        attempt++;
        clearInterval(cooldownTimer);
        activeCheckout?.close();
        activeCheckout = null;
      },
      syncFailureDialog() {
        const dialog = this.$refs.paymentIssue;
        if (!dialog) return;
        if (this.paymentFailed && !this.pendingPayment && !this.gatewayOpen) {
          if (!dialog.open) dialog.showModal();
        } else if (dialog.open) dialog.close();
      },
      showFailure(message) {
        this.error = message;
        this.amountLimited = /amount.*(?:maximum|limit)|(?:maximum|limit).*amount/i.test(message);
        this.paymentFailed = true;
      },
      get failureHelp() {
        return this.amountLimited
          ? 'This payment exceeds the limit allowed by the payment provider or selected method. Choose Pay on Delivery, or return to your cart and change the total before trying online payment again.'
          : 'Try again, switch to Pay on Delivery, or return to your cart.';
      },
      get buttonLabel() {
        return this.cooldown > 0 && this.method === 'razorpay'
          ? `Please wait ${this.cooldown}s`
          : this.pendingPayment || this.pendingAttempt
          ? 'Retry order confirmation'
          : `${this.method === 'cod' ? 'Place cash order' : 'Continue with Razorpay'}  ·  ₹${(this.amount / 100).toLocaleString('en-IN', { maximumFractionDigits: 2 })}`;
      },
      async request(url, body) {
        const controller = new AbortController();
        const timer = setTimeout(() => controller.abort(), 30000);
        try {
          const response = await fetch(url, {
            method: 'POST', signal: controller.signal,
            headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
            body: JSON.stringify(body),
          });
          // Keep the deadline active while reading the response body as well.
          let result;
          try { result = await response.json(); } catch (err) {
            if (err.name === 'AbortError') throw err;
            throw new Error('The payment service did not respond correctly. Wait a moment and retry to check your payment status.');
          }
          if (!response.ok) {
            const retry = Number(response.headers?.get('Retry-After'));
            if (retry > 0) {
              this.cooldown = Math.min(600, Math.ceil(retry));
              clearInterval(cooldownTimer);
              cooldownTimer = setInterval(() => {
                if (--this.cooldown <= 0) clearInterval(cooldownTimer);
              }, 1000);
            }
            if (result.payment_pending) this.pendingAttempt = true;
            throw new Error(result.error || 'Could not confirm payment status. Try again shortly.');
          }
          return result;
        } catch (err) {
          if (err.name === 'AbortError') throw new Error('The connection is taking too long. Wait a moment and retry to check the same payment attempt.');
          throw err;
        } finally { clearTimeout(timer); }
      },
      rememberPayment(payment) {
        this.pendingPayment = payment;
        this.pendingAttempt = false;
        try { sessionStorage.setItem('uniminute-pending-payment', JSON.stringify({ amount: this.amount, payment, expires: Date.now() + 30 * 60 * 1000 })); } catch {}
      },
      async pay() {
        if (this.busy || this.gatewayOpen || this.cooldown > 0) return;
        const current = ++attempt;
        this.error = '';
        this.paymentFailed = false;
        this.amountLimited = false;
        // Close our dialog synchronously before opening the payment provider.
        this.syncFailureDialog();
        this.busy = true;
        try {
          if (this.pendingPayment) {
            await this.verify(this.pendingPayment);
            return;
          }
          if (!window.Razorpay) throw new Error('Secure checkout did not load. Check your connection and try again.');
          const order = await this.request('/api/create-order', { amount: this.amount, currency: 'INR', receipt: `cart-${Date.now()}` });
          if (current !== attempt) return;
          if (order.payment) {
            this.rememberPayment(order.payment);
            await this.verify(order.payment);
            return;
          }
          this.pendingAttempt = false;
          let failure = '';
          let completed = false;
          const checkout = new Razorpay({
            key: order.key_id,
            amount: order.amount,
            currency: order.currency,
            order_id: order.order_id,
            name: 'Uniminute',
            description: 'Cart payment',
            // Our dialog owns retries. Razorpay closes before it is displayed.
            retry: { enabled: false },
            handler: async (payment) => {
              if (current !== attempt || completed) return;
              completed = true;
              this.rememberPayment(payment);
              ++attempt;
              activeCheckout = null;
              this.gatewayOpen = false;
              try {
                await this.verify(payment);
              } catch (err) {
                this.error = `${err.message || 'Payment could not be verified.'} Use “Retry order confirmation” — do not pay again.`;
                this.busy = false;
                lwToast(this.error, 'error');
              }
            },
            modal: {
              ondismiss: () => {
                if (current !== attempt || completed) return;
                ++attempt;
                activeCheckout = null;
                this.gatewayOpen = false;
                this.busy = false;
                if (!this.pendingPayment) this.showFailure(failure || 'Payment was cancelled. No order was placed.');
              },
            },
            theme: { color: '#143E32' },
          });
          checkout.on('payment.failed', (event) => {
            if (current !== attempt || completed || failure) return;
            failure = event?.error?.description || 'Payment failed. Check the details and try again.';
            // With SDK retry disabled, ondismiss follows this event. Do not
            // put another modal over a Razorpay frame that is still closing.
          });
          activeCheckout = checkout;
          this.gatewayOpen = true;
          checkout.open();
        } catch (err) {
          if (current !== attempt) return;
          ++attempt;
          const checkout = activeCheckout;
          activeCheckout = null;
          checkout?.close();
          this.gatewayOpen = false;
          this.busy = false;
          const message = err.message || 'Could not start payment. Try again.';
          if (this.pendingPayment) {
            this.error = `${message} Use “Retry order confirmation” — do not pay again.`;
            lwToast(this.error, 'error');
          } else if (this.pendingAttempt) {
            this.error = message;
          } else this.showFailure(message);
        }
      },
      place(cashForm) {
        this.error = '';
        if (this.pendingPayment || this.pendingAttempt || this.method === 'razorpay') {
          this.pay();
          return;
        }
        if (this.cashBusy || !cashForm) return;
        this.cashBusy = true;
        lwProcessing('Placing your order…', false);
        cashForm.requestSubmit();
      },
      chooseCash() {
        if (this.pendingPayment || this.pendingAttempt) return;
        this.method = 'cod';
        this.cancelFailure();
      },
      cancelFailure() {
        this.paymentFailed = false;
        this.error = '';
        this.syncFailureDialog();
      },
      async verify(payment) {
        const result = await this.request('/api/verify-payment', payment);
        this.pendingPayment = null;
        try { sessionStorage.removeItem('uniminute-pending-payment'); } catch {}
        this.paymentFailed = false;
        lwProcessing('Payment confirmed. Placing your order…', false);
        location.assign(result.redirect || '/orders');
      },
    };
  });

  // ── sign-in card (LoginScreen) ───────────────────────────────────────────
  // One field per step; the button only wakes for something that could be
  // right, and the line under it says why it will not.
  Alpine.data('login', (step, email, error, resendAt = 0) => ({
    now: Date.now(),
    timer: null,
    init() {
      if (resendAt > this.now) this.timer = setInterval(() => {
        this.now = Date.now();
        if (this.now >= resendAt) { clearInterval(this.timer); this.timer = null; }
      }, 250);
    },
    destroy() { if (this.timer) clearInterval(this.timer); },
    get remaining() { return Math.max(0, Math.ceil((resendAt - this.now) / 1000)); },
    value: step === 'email' ? email : '',
    password: '', // the new one, on the reset step
    busy: false,
    error,
    get valid() {
      const v = this.value.trim();
      if (step === 'email') return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(v);
      if (step === 'code') return /^\d{6}$/.test(v);
      if (step === 'reset') return /^\d{6}$/.test(v) && this.password.length >= 8;
      return this.value.length > 0;
    },
    get helper() {
      if (this.error) return this.error;
      const v = this.value.trim();
      if (v && !this.valid) {
        if (step === 'reset' && /^\d{6}$/.test(v)) {
          return this.password ? 'A password needs at least 8 characters.' : 'Now choose a new password.';
        }
        return {
          email: 'That is not an email address yet.',
          code: 'Enter the six digits from your email.',
          reset: 'Enter the six digits from your email.',
          password: 'Enter your password to sign in.',
        }[step];
      }
      return {
        email: 'New here? We will email you a code to get started.',
        code: `We sent a code to ${email}. It expires in 10 minutes.`,
        reset: `We sent a password reset code to ${email}. Enter it and a new password.`,
        password: `Signing in as ${email}.`,
      }[step];
    },
  }));

  // A live profile card with five keyboard-friendly steps. Passwords stay in
  // memory only and are cleared after an acknowledged save.
  Alpine.data('profileSetup', (cfg) => ({
    ...cfg,
    step: 0, password: '', revealPassword: false, saving: false, complete: false,
    error: '', saveKey: '',
    init() {
      this.setPhone(this.phone);
      if (!this.groups.some(g => g.id === this.interest)) this.interest = 'everyday';
      if (!this.options.some(a => a.id === this.avatar)) this.avatar = this.options[0].id;
      this.$watch('avatar', () => this.$nextTick(() => {
        if (!matchMedia('(prefers-reduced-motion: reduce)').matches) {
          this.$el.querySelector('.profile-card-avatar')?.animate([{opacity:.55},{opacity:1}], {duration:140,easing:'ease-out'});
        }
      }));
    },
    get options() { return this.groups.find(g => g.id === this.interest)?.avatars || []; },
    get interestLabel() { return this.groups.find(g => g.id === this.interest)?.label || 'Everyday shopper'; },
    get avatarURL() { return '/static/images/avatars/' + this.avatar + '.png'; },
    get progress() { return this.complete ? 100 : [20,40,60,80,95][this.step]; },
    get stepLabel() { return ['Your avatar','Your name','Mobile number','Optional password','Delivery address'][this.step]; },
    get title() { return ['Make it yours.','What should we call you?','Let’s stay in touch.','Keep a little secret.','Where should we deliver?'][this.step]; },
    get description() { return [
      'Pick a world you love. Then choose a face for your card.',
      'Your name makes this card yours.',
      'So your rider can reach you when your order arrives.',
      'Add a password to sign in without waiting for an email code.',
      'The last detail. Tell us where to bring your favourites.',
    ][this.step]; },
    phoneDigits(value) {
      const raw = String(value || '').trim();
      let digits = raw.replace(/\D/g, '');
      if (raw.startsWith('+91') || (digits.length === 12 && digits.startsWith('91'))) digits = digits.slice(2);
      return digits;
    },
    setPhone(value) { this.phone = this.phoneDigits(value).slice(0, 10); },
    pastePhone(event) {
      if (!event.clipboardData) return;
      event.preventDefault();
      const input = event.target, start = input.selectionStart ?? 0, end = input.selectionEnd ?? this.phone.length;
      const digits = this.phoneDigits(event.clipboardData.getData('text'));
      this.setPhone(this.phone.slice(0, start) + digits + this.phone.slice(end));
      input.value = this.phone;
      const caret = Math.min(start + digits.length, this.phone.length);
      input.setSelectionRange(caret, caret);
    },
    get mobile() { return this.phoneDigits(this.phone); },
    get displayPhone() {
      const digits = this.mobile.slice(0, 10);
      if (!digits) return '';
      return '+91 ' + digits.slice(0, 2) + 'X'.repeat(Math.min(4, Math.max(0, digits.length - 2))) + digits.slice(6);
    },
    chooseInterest(id) {
      if (this.saving) return;
      const group = this.groups.find(g => g.id === id);
      if (!group) return;
      this.interest = id;
      if (!group.avatars.some(a => a.id === this.avatar)) this.avatar = group.avatars[0].id;
      this.saveKey = ''; this.error = '';
    },
    focusStep() {
      this.$nextTick(() => {
        const input = [null,this.$refs.nameInput,this.$refs.phoneInput,this.hasPassword ? null : this.$refs.passwordInput,this.$refs.addressInput][this.step];
        (input || this.$refs.stepTitle)?.focus({preventScroll:true});
        input?.scrollIntoView({block:'nearest',behavior:'instant'});
      });
    },
    back() { if (this.saving || this.step===0) return; this.step--; this.error=''; this.revealPassword=false; this.focusStep(); },
    validation() {
      if (this.step===1 && (!this.name.trim() || Array.from(this.name.trim()).length>100 || /[<>]/.test(this.name))) return 'Enter your name, up to 100 characters.';
      if (this.step===2 && !/^[6-9]\d{9}$/.test(this.mobile)) return 'Enter a valid 10-digit Indian mobile number.';
      if (this.step===3 && !this.hasPassword && this.password && (this.password.length<8 || this.password.length>200)) return 'Use at least 8 characters, or leave the password empty to skip.';
      if (this.step===4 && (!this.line.trim() || Array.from(this.line.trim()).length>300 || /[<>]/.test(this.line))) return 'Add your hostel and room, or block and shop, up to 300 characters.';
      if (this.step===4 && !this.city) return 'Delivery locations are unavailable. Reload this page and try again.';
      return '';
    },
    async advance() {
      if (this.saving || this.complete) return;
      this.error=this.validation();
      if (this.error) { this.focusStep(); return; }
      if (this.step<4) { this.step++; this.revealPassword=false; this.focusStep(); return; }
      await this.save();
    },
    async savedAfterTimeout() {
      const [me,addresses] = await Promise.all([fetch('/api/me',{cache:'no-store'}),fetch('/api/addresses',{cache:'no-store'})]);
      if (!me.ok || !addresses.ok) return false;
      const user=await me.json(), list=await addresses.json();
      return user.ready && user.name===this.name.trim() && user.phone===this.mobile && user.avatar===this.avatar &&
        (!this.password || user.hasPassword) && Array.isArray(list) && list.some(a=>a.line===this.line.trim() && a.city===this.city);
    },
    finish(target) {
      this.complete=true; this.password=''; this.revealPassword=false;
      if (target && target.startsWith('/') && !target.startsWith('//')) this.next=target;
      this.$nextTick(()=>{ this.$refs.successTitle?.focus({preventScroll:true}); window.scrollTo({top:0,behavior:'instant'}); });
    },
    async save() {
      if (this.saving || this.complete) return;
      this.saving=true; this.error=''; this.saveKey ||= crypto.randomUUID();
      const controller=new AbortController(), timer=setTimeout(()=>controller.abort(),20000);
      const body=new URLSearchParams({name:this.name.trim(),phone:this.mobile,line:this.line.trim(),label:this.label,city:this.city,avatar:this.avatar,next:this.next,setupRequestId:this.saveKey});
      if (!this.hasPassword && this.password) body.set('password',this.password);
      try {
        const response=await fetch('/profile/setup',{method:'POST',headers:{Accept:'application/json'},body,signal:controller.signal});
        const result=await response.json();
        if (!response.ok || !result.saved) { this.error=result.error || 'Your profile could not be saved. Please try again.'; return; }
        this.finish(result.redirect);
      } catch (err) {
        try { if (await this.savedAfterTimeout()) { this.finish(this.next); return; } } catch {}
        this.error=err.name==='AbortError' ? 'The connection is taking too long. Your details are still here; try again.' : 'We could not confirm your save. Check your connection and try again.';
      } finally { clearTimeout(timer); this.saving=false; }
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
        lwLeave('/seller');
      } catch (err) {
        lwToast(err.message, 'error');
        this.saving = false;
      }
    },
  }));

  // ── admin panel (AdminScreen) ────────────────────────────────────────────
  // One dialog, many decisions: `dlg` is its content, `dlg.run` its action.
  Alpine.data('adminSecuritySettings', (cfg) => ({
    enabled: !!cfg?.enabled, configured: !!cfg?.configured,
    remaining: cfg?.recoveryCodesRemaining || 0,
    password: '', code: '', secret: '', uri: '', recoveryCodes: [],
    busy: false, error: '', notice: '',
    async run(action) {
      if (this.busy) return;
      this.busy = true; this.error = ''; this.notice = '';
      try {
        const result = await sellerCall('POST', '/staff-api/admin/security/mfa', { action, password: this.password, code: this.code.trim() });
        if (action === 'begin') { this.secret = result.secret; this.uri = result.uri; this.code = ''; }
        else {
          this.enabled = !!result.enabled; this.secret = ''; this.uri = ''; this.password = ''; this.code = '';
          this.recoveryCodes = result.recoveryCodes || []; this.remaining = this.recoveryCodes.length;
          this.notice = this.enabled ? 'Authenticator enabled. Save your recovery codes below.' : 'Authenticator disabled.';
        }
      } catch (err) { this.error = err.message || 'Could not update security settings. Try again.'; }
      finally { this.busy = false; }
    },
    downloadCodes() {
      const blob = new Blob(['Uniminute admin recovery codes. Keep these private. Each code works once.\n\n' + this.recoveryCodes.join('\n')], { type: 'text/plain' });
      const href = URL.createObjectURL(blob); const a = document.createElement('a');
      a.href = href; a.download = 'uniminute-admin-recovery-codes.txt'; a.click(); setTimeout(() => URL.revokeObjectURL(href), 1000);
    },
  }));

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
    openBuyerDelete(id, email) {
      this.open({kind:'delete-buyer',title:'Delete this buyer account?',id,email,confirmEmail:'',password:'',error:'',danger:true,action:'Delete user',busyLabel:'Deleting…',
        ready:()=>this.dlg.confirmEmail.trim()===this.dlg.email && this.dlg.password.length>0,
        run:()=>this.deleteBuyer(),
      });
    },
    async deleteBuyer() {
      if (this.dlg.busy || !this.dlg.ready()) return;
      this.dlg.busy=true;this.dlg.error='';
      try {
        await sellerCall('DELETE',`/staff-api/admin/people/${encodeURIComponent(this.dlg.id)}`,{email:this.dlg.confirmEmail.trim(),password:this.dlg.password});
        this.dlg.password='';this.$refs.dlg.close();lwReload('Buyer account deleted. Their next sign-in starts a new profile.');
      } catch(err) {this.dlg.error=err.message;this.dlg.password='';}
      finally {this.dlg.busy=false;}
    },
    suspendStore(owner, name) {
      this.open({kind:'suspend-store',title:'Suspend this store?',body:`${name} and its products will be hidden from shoppers. New products and uploads will be blocked. The account, stock and existing orders remain available.`,value:'',danger:true,action:'Suspend store',busyLabel:'Suspending…',
        ready:()=>this.dlg.value.trim().length>0,
        run:()=>this.done('POST',`/stores/${encodeURIComponent(owner)}/suspend`,{reason:this.dlg.value.trim()},'Store suspended.'),
      });
    },
    restoreStore(owner, name) {
      this.confirm('Restore this store?',`${name} will be visible to shoppers again and its owner can add products. The existing inventory is retained.`,'Restore store',()=>this.done('POST',`/stores/${encodeURIComponent(owner)}/restore`,{},'Store restored.'));
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
      this.open({
        kind: 'newpin', title: `New PIN for ${phone}`, value: '', action: 'Save PIN',
        ready: () => this.dlg.value === '' || /^\d{6}$/.test(this.dlg.value),
        run: async () => {
          const pin = this.dlg.value.trim();
          const out = await this.call('POST', `/riders/${phone}/pin`, pin ? { pin } : {});
          if (out !== null) this.showPin(out.pin || '', phone);
        },
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
        lwLeave('/admin?tab=banners');
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
        lwLeave('/admin?tab=policies');
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
    price: cfg.price ? Number(cfg.price).toLocaleString('en-IN', { maximumFractionDigits: 2 }) : '',
    mrp: cfg.mrp > 0 ? Number(cfg.mrp).toLocaleString('en-IN', { maximumFractionDigits: 2 }) : '',
    stock: cfg.id ? String(cfg.stock) : '',
    shots: cfg.imageUrls.map((url) => ({ url })),
    options: cfg.options.map((o) => ({ name: o.name, kind: o.kind || '', values: [...(o.values || [])], entry: '' })),
    variantPriceInputs: Object.fromEntries((cfg.variantPrices || []).map((v) => [JSON.stringify(v.choices), Number(v.price).toLocaleString('en-IN', { maximumFractionDigits: 2 })])),
    variantMRPInputs: Object.fromEntries((cfg.variantPrices || []).filter((v) => v.mrp > 0).map((v) => [JSON.stringify(v.choices), Number(v.mrp).toLocaleString('en-IN', { maximumFractionDigits: 2 })])),
    attrs: { ...cfg.attributes },
    section: '',
    busy: false,
    saving: false,
    classification: null,
    classificationRequest: 0,
    init() {
      if (!this.category && this.sections.length) this.category = this.sections[0].leaves[0];
      this.section = this.sectionOf(this.category) || (this.sections[0] ? this.sections[0].name : '');
      this.$watch('category', () => this.checkClassification());
      if (this.title.trim()) this.checkClassification();
      // Existing single-price listings retain their price when the matrix is first edited.
      if (this.id && !(cfg.variantPrices || []).length) {
        for (const row of this.variantRows) this.variantPriceInputs[row.key] = this.price;
      }
    },
    async checkClassification() {
      const request = ++this.classificationRequest;
      this.classification = null;
      if (!this.title.trim() || !this.category) return;
      try {
        const result = await sellerCall('POST', '/api/seller/classify', { title: this.title.trim(), category: this.category });
        if (request === this.classificationRequest) this.classification = result;
      } catch { /* Saving remains available if the preview service is offline. */ }
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
    get priceValue() { const t = rupees(this.price); return t === '' ? NaN : Number(t); },
    get mrpValue() { const t = rupees(this.mrp); return t === '' ? 0 : Number(t); },
    groupRupees(v) {
      const n = Number(rupees(v));
      return rupees(v) === '' || Number.isNaN(n) ? v : n.toLocaleString('en-IN', { maximumFractionDigits: 2 });
    },
    get stockValue() { const t = this.stock.trim(); return /^\d+$/.test(t) ? Number(t) : NaN; },
    get variantRows() {
      const groups = this.liveOptions();
      if (!groups.length) return [];
      let rows = [{ choices: [] }];
      for (const group of groups) {
        rows = rows.flatMap((row) => group.values.map((value) => ({ choices: [...row.choices, { name: group.name, value }] })));
        if (rows.length > 100) return [];
      }
      return rows.map((row) => ({ ...row, key: JSON.stringify(row.choices), label: row.choices.map((c) => `${c.name}: ${c.name.toLowerCase() === 'colour' ? this.swatchName(c.value) : c.value}`).join(' · ') }));
    },
    get variantCount() { return this.liveOptions().reduce((n, o) => n * o.values.length, 1); },
    // A row's own MRP, or the listing-wide one when that row is left blank.
    rowMRP(key) {
      const own = rupees(this.variantMRPInputs[key] || '');
      return own === '' ? this.mrpValue || 0 : Number(own);
    },
    rowPrice(key) { return Number(rupees(this.variantPriceInputs[key] || '')); },
    rowDiscount(key) {
      const mrp = this.rowMRP(key), price = this.rowPrice(key);
      if (!(mrp > 0) || !(price > 0) || mrp <= price) return '';
      return `${Math.round((mrp - price) / mrp * 100)}% OFF — ₹${(mrp - price).toLocaleString('en-IN', { maximumFractionDigits: 2 })} saved`;
    },
    liveVariantPrices() {
      return this.variantRows.map((row) => ({ choices: row.choices, price: this.rowPrice(row.key), mrp: this.rowMRP(row.key) }));
    },
    get blocker() {
      if (this.busy) return 'Wait for your photos to finish';
      if (!this.shots.length) return 'Add at least one photo';
      if (!this.title.trim()) return 'Give the product a title';
      if ([...this.title.trim()].length > 250) return 'Shorten the title to 250 characters or fewer';
      if (this.liveOptions().length && this.variantCount > 100) return 'Use at most 100 option combinations';
      if (this.variantRows.some((row) => !(Number(rupees(this.variantPriceInputs[row.key] || '')) > 0))) return 'Set a price for every option combination';
      if (!this.variantRows.length && !(this.priceValue > 0)) return 'Set a price above ₹0';
      if (this.variantRows.some((row) => this.rowMRP(row.key) > 0 && this.rowMRP(row.key) < this.rowPrice(row.key))) return 'A combination MRP cannot be below its own price';
      if (this.variantRows.some((row) => Number.isNaN(this.rowMRP(row.key)))) return 'Each combination MRP must be a number, or left blank';
      if (Number.isNaN(this.mrpValue)) return 'MRP must be a number, or left blank';
      if (!this.variantRows.length && this.mrpValue > 0 && this.mrpValue < this.priceValue) return 'MRP cannot be below the selling price';
      if (!(this.stockValue >= 0)) return 'Enter how many units you have';
      return null;
    },
    get discount() {
      if (this.variantRows.length) {
        // The card shows the cheapest combination, so it shows that row's MRP.
        const cheapest = this.liveVariantPrices().filter((v) => v.price > 0).sort((a, b) => a.price - b.price)[0];
        if (!cheapest) return null;
        const text = this.rowDiscount(JSON.stringify(cheapest.choices));
        return text ? { bad: false, text: `Buyers see ${text} on the starting price.` } : null;
      }
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
    // A colour value is a preset hex ("#2F6FED") or a custom "Name|#hex". Mirrors shop.ColourParts.
    swatchName(v) {
      const [name, hex] = String(v).includes('|') ? String(v).split('|') : ['', v];
      if (name.trim()) return name.trim();
      const s = this.swatches.find((s) => s.hex.toLowerCase() === String(hex).toLowerCase());
      return s ? s.name : v;
    },
    colourHex(v) { return String(v).includes('|') ? String(v).split('|')[1] : v; },
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
      if (this.blocker || this.saving || this.busy) return;
      this.saving = true;
      lwProcessing(this.id ? 'Saving product…' : 'Uploading product photos…');
      const fields = {
        title: this.title.trim(), description: this.description.trim(), category: this.category,
        price: this.variantRows.length ? Math.min(...this.liveVariantPrices().map((v) => v.price)) : this.priceValue,
        mrp: this.mrpValue, compareGroup: this.compareGroup, stock: this.stockValue,
      };
      try {
        if (this.id) {
          await sellerCall('PATCH', `/api/seller/items/${this.id}`, { ...fields, options: this.liveOptions(), variantPrices: this.liveVariantPrices(), attributes: this.liveAttrs() });
        } else {
          const body = new FormData();
          for (const [k, v] of Object.entries(fields)) body.set(k, v);
          body.set('options', JSON.stringify(this.liveOptions()));
          body.set('variantPrices', JSON.stringify(this.liveVariantPrices()));
          body.set('attributes', JSON.stringify(this.liveAttrs()));
          this.shots.forEach((s) => s.file && body.append('file', s.file));
          await sellerCall('POST', '/api/seller/items', body, (percent) => lwProcessing(percent === 100 ? 'Photos uploaded. Saving product…' : `Uploading product photos… ${percent}%`));
        }
        this.saving = false;
        lwProcessing('');
        await lwResult({ title: this.id ? 'Product updated' : 'Added to inventory', body: `“${fields.title}” has been saved successfully.`, ok: 'View inventory' });
        lwLeave('/seller?pane=inventory');
      } catch (err) {
        this.saving = false;
        lwProcessing('');
        await lwResult({ title: 'Product could not be saved', body: `${err.message} Your form is still here. Check your inventory before retrying if the connection was interrupted.`, danger: true, ok: 'Back to product' });
      }
    },
  }));

  // ── seller: dashboard actions (SellerDashboardScreen) ────────────────────
  Alpine.data('dailyAvailability', (off, kind) => ({
    off, kind, busy: false, error: '', saved: '', revision: 0, resetTimer: null, onReturn: null,
    get path() { return this.kind === 'store' ? '/api/seller/availability' : '/staff-api/delivery/availability'; },
    init() {
      this.onReturn = () => { if (document.visibilityState === 'visible') return this.refresh(); };
      document.addEventListener('visibilitychange', this.onReturn);
      window.addEventListener('online', this.onReturn);
      this.scheduleReset();
    },
    destroy() {
      clearTimeout(this.resetTimer);
      document.removeEventListener('visibilitychange', this.onReturn);
      window.removeEventListener('online', this.onReturn);
    },
    scheduleReset(retry = false) {
      clearTimeout(this.resetTimer);
      const now = Date.now(), ist = new Date(now + 19800000);
      const midnight = Date.UTC(ist.getUTCFullYear(), ist.getUTCMonth(), ist.getUTCDate() + 1) - 19800000;
      this.resetTimer = setTimeout(async () => {
        const refreshed = await this.refresh();
        this.scheduleReset(!refreshed);
      }, retry ? 30000 : Math.max(1000, midnight - now + 1000));
    },
    async refresh() {
      if (this.busy) return false;
      const revision = ++this.revision;
      try {
        const out = await sellerCall('GET', this.path);
        if (!this.busy && revision === this.revision) {
          this.off = this.kind === 'store' ? out.closedToday : out.dayOffToday;
          this.saved = ''; this.error = '';
        }
        return true;
      } catch (err) {
        if (!this.busy && revision === this.revision) this.error = 'Could not refresh availability. Check your connection and refresh this page.';
        return false;
      }
    },
    get onTitle() { return this.kind === 'store' ? 'Store is open' : 'You are online'; },
    get offTitle() { return this.kind === 'store' ? 'Closed today' : 'Day off today'; },
    get onHelp() { return this.kind === 'store' ? 'Accepting new orders.' : 'Ready to collect packages from shops.'; },
    get offHelp() { return this.kind === 'store' ? 'New orders are paused until midnight IST.' : 'New assignments are paused until midnight IST.'; },
    async toggle() {
      if (this.busy) return;
      this.busy = true; this.revision++; this.error = ''; this.saved = '';
      try {
        const out = await sellerCall('PUT', this.path, {available: this.off});
        this.off = this.kind === 'store' ? out.closedToday : out.dayOffToday;
        this.saved = this.off ? 'Saved. Back ON at midnight IST.' : 'Saved. You are back ON.';
        if (this.kind === 'rider') {
          const orders = document.getElementById('delivery-orders');
          if (orders && window.htmx) htmx.trigger(orders, 'delivery-orders-refresh');
        }
      } catch (err) { this.error = err.message || 'Could not save availability. Please try again.'; }
      finally { this.busy = false; }
    }
  }));

  Alpine.data('sellerDashboard', (pane) => ({
    pane,
    shownOrders: 8,
    shownItems: 8,
    rejecting: null,
    reason: '',
    removing: null,
    init() {
      this.$watch('pane', (v) => {
        history.replaceState(null, '', `?pane=${v}`);
        if (v === 'orders') this.$nextTick(() => {
          const content = document.getElementById('seller-orders-content');
          if (content) htmx.trigger(content, 'seller-orders-refresh');
        });
      });
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
window.sellerCall = async (method, path, body, onProgress) => {
  if (onProgress && body instanceof FormData) return lwUpload(method, path, body, onProgress);
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
  async addFiles(e) {
    const files = [...e.target.files];
    e.target.value = '';
    if (!files.length || this.busy || this.saving) return;
    this.busy = true;
    lwProcessing('Preparing product photos…');
    try {
      // Validate the whole batch before changing the selected photos.
      const prepared = [];
      for (const file of files) prepared.push(await lwPreparePhoto(file));
      const total = [...this.shots.filter((s) => s.file).map((s) => s.file), ...prepared].reduce((n, f) => n + f.size, 0);
      if (total > 35 * 1024 * 1024) throw new Error('These photos are too large together. Keep the selected photos below 35 MB, or choose fewer photos.');
      await this.setShots([...this.shots, ...prepared.map((file) => ({ file, preview: URL.createObjectURL(file) }))]);
    } catch (err) {
      lwProcessing('');
      await lwResult({ title: 'Photo could not be added', body: err.message, danger: true, ok: 'Choose another photo' });
    } finally {
      this.busy = false;
      lwProcessing('');
    }
  },
  remove(i) {
    if (this.busy || this.saving) return;
    const s = [...this.shots]; s.splice(i, 1); this.setShots(s);
  },
  move(i, step) {
    if (this.busy || this.saving) return;
    const j = i + step;
    if (j < 0 || j >= this.shots.length) return;
    const s = [...this.shots];
    [s[i], s[j]] = [s[j], s[i]];
    this.setShots(s);
  },
  async setShots(next) {
    const previous = this.shots;
    this.shots = next;
    if (!this.id) return;
    this.busy = true;
    lwProcessing('Uploading product photos…');
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
        lwProcessing('');
        await lwResult({ title: 'Some photos could not be uploaded', body: `${fresh.length - failed.length} of ${fresh.length} photos added. ${failed.length} failed: ${lastError}`, danger: true });
      } else if (failed.length) {
        lwProcessing('');
        await lwResult({ title: 'Photo upload failed', body: lastError, danger: true });
      } else if (fresh.length) {
        lwToast(fresh.length === 1 ? 'Photo added.' : `${fresh.length} photos added.`, 'success');
      }
    } catch (err) {
      this.shots = previous;
      lwProcessing('');
      await lwResult({ title: 'Photos could not be saved', body: err.message, danger: true });
    } finally {
      this.busy = false;
      lwProcessing('');
    }
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
      window.lwProcessing = (title, protectNavigation = true) => {
        this.active = !!title;
        this.title = title || '';
        if (title && protectNavigation) window.addEventListener('beforeunload', guard);
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
  let menuTimer;
  const menuScreen = () => document.getElementById('lw-menu-navigation');
  const dismissMenu = () => {
    clearTimeout(menuTimer);
    if (menuScreen()) menuScreen().hidden = true;
  };
  document.addEventListener('click', (event) => {
    const link = event.target.closest?.('a[href]');
    if (!link || event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || link.hasAttribute('download') || (link.target && link.target !== '_self')) return;
    const url = new URL(link.href, location.href);
    if (url.origin !== location.origin || !url.pathname.startsWith('/store/') || (url.pathname === location.pathname && url.search === location.search)) return;
    if (!navigator.onLine) return;
    const screen = menuScreen();
    if (!screen) return;
    screen.hidden = false;
    screen.querySelector('[data-menu-loading-message]').textContent = 'Loading store menu…';
    menuTimer = setTimeout(() => {
      screen.querySelector('[data-menu-loading-message]').textContent = 'Your connection is taking a little longer…';
    }, 8000);
    show();
  });
  document.getElementById('lw-menu-cancel')?.addEventListener('click', () => {
    window.stop();
    dismissMenu();
    hide();
  });
  window.addEventListener('pageshow', dismissMenu);
  window.addEventListener('beforeunload', show);
  window.addEventListener('pageshow', hide);
})();

// Error and offline: HTMX requests that never reached the server, or failed.
document.addEventListener('htmx:sendError', () => {
  if (window.lwProcessing) lwProcessing('');
  lwToast(navigator.onLine ? 'Could not reach Uniminute. Try again in a moment.' : "You're offline. Reconnect and try again.", 'error');
});
document.addEventListener('htmx:responseError', (e) => {
  if (window.lwProcessing) lwProcessing('');
  const status = e.detail.xhr.status;
  // The server already said why, in its own words.
  if ((e.detail.xhr.getResponseHeader('HX-Trigger') || '').includes('lw:toast')) return;
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
document.addEventListener('htmx:timeout', () => {
  if (window.lwProcessing) lwProcessing('');
  lwToast('That is taking too long. Your order may still have been placed—check My Orders before trying again.', 'error');
});

// Permission required: the browser's notification prompt, asked for on purpose.
document.addEventListener('alpine:init', () => {
  Alpine.data('notifyPermission', (subscribeURL = '/api/push/subscribe') => ({
    state: 'Notification' in window ? Notification.permission : 'unsupported',
    busy: false,
    error: '',
    init() {
      if (this.state === 'granted') this.sync(false);
    },
    async ask() {
      if (this.busy || this.state === 'unsupported') return;
      this.busy = true;
      this.error = '';
      try {
        this.state = await Notification.requestPermission();
        if (this.state === 'granted') await this.sync(true);
      } catch (err) {
        this.error = err.message || 'Notifications could not be turned on.';
        lwToast(this.error, 'error');
      } finally {
        this.busy = false;
      }
    },
    async sync(announce) {
      try {
        await window.lwEnablePush(subscribeURL);
        if (announce) lwToast('Order notifications are on.', 'success');
      } catch (err) {
        this.error = err.message || 'Notifications could not be connected.';
        if (announce) throw err;
      }
    },
  }));
});

let lwMessaging = null;
window.lwEnablePush = async (subscribeURL = '/api/push/subscribe') => {
  if (!('serviceWorker' in navigator) || !('Notification' in window)) {
    throw new Error('This browser does not support push notifications.');
  }
  const cfgRes = await fetch('/api/push/config', { headers: { Accept: 'application/json' } });
  if (!cfgRes.ok) {
    throw new Error(cfgRes.status === 503
      ? 'Push notifications are not configured on this server yet.'
      : 'Could not load notification settings. Try again.');
  }
  const cfg = await cfgRes.json();
  const registration = await navigator.serviceWorker.register('/firebase-messaging-sw.js', { scope: '/' });
  const [{ initializeApp, getApps }, { getMessaging, getToken, onMessage, isSupported }] = await Promise.all([
    import('https://www.gstatic.com/firebasejs/12.19.0/firebase-app.js'),
    import('https://www.gstatic.com/firebasejs/12.19.0/firebase-messaging.js'),
  ]);
  if (!(await isSupported())) throw new Error('This browser does not support Firebase notifications.');
  const app = getApps().length ? getApps()[0] : initializeApp(cfg.firebase);
  const messaging = getMessaging(app);
  const token = await getToken(messaging, { vapidKey: cfg.publicKey, serviceWorkerRegistration: registration });
  if (!token) throw new Error('The browser did not create a notification token.');
  const saved = await fetch(subscribeURL, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ token }),
  });
  if (!saved.ok) {
    let message = 'Could not save notification settings.';
    try { message = (await saved.json()).error || message; } catch {}
    throw new Error(message);
  }
  if (!lwMessaging) {
    lwMessaging = messaging;
    onMessage(messaging, (payload) => {
      const data = payload?.data || {};
      lwToast(data.body || data.title || 'Your order has an update.', 'success');
    });
  }
  return token;
};

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

// Back/forward cache: htmx keeps a form's busy state while HX-Redirect leaves
// the page, so the snapshot the browser restores on Back is faded and
// unclickable. Clear every in-flight look when a page comes back that way.
window.addEventListener('pageshow', (e) => {
  if (!e.persisted) return;
  document.querySelectorAll('.htmx-request').forEach((el) => el.classList.remove('htmx-request'));
  if (window.lwProcessing) lwProcessing('');
});

// Leaving a form after a save. A plain navigation would stack the page on top
// of the form (List → Edit → List), so Back returned to the form. Instead:
// if the form was opened from that page, step back to it (and refresh it);
// otherwise replace the form, so it never stays in history.
window.lwLeave = (target) => {
  let from = null;
  try { from = new URL(document.referrer); } catch {}
  const to = new URL(target, location.href);
  if (from && from.origin === location.origin && from.pathname === to.pathname && history.length > 1) {
    try { sessionStorage.setItem('lw:refresh-on-back', to.pathname); } catch {}
    history.back();
  } else {
    location.replace(to.href);
  }
};

// Server-side saves answer HX-Redirect; send those through lwLeave too.
document.addEventListener('htmx:beforeOnLoad', (e) => {
  const target = e.detail.xhr.getResponseHeader('HX-Redirect');
  if (!target) return;
  e.preventDefault();
  if (e.detail.xhr.responseURL && new URL(e.detail.xhr.responseURL).pathname.startsWith('/login/')) {
    lwProcessing('Loading your account…', false);
    location.replace(target);
    return;
  }
  lwLeave(target);
});

// The page stepped back to may come from the back/forward cache: reload it so
// it shows what was just saved.
window.addEventListener('pageshow', (e) => {
  let want = null;
  try { want = sessionStorage.getItem('lw:refresh-on-back'); sessionStorage.removeItem('lw:refresh-on-back'); } catch {}
  if (want === location.pathname && e.persisted) location.reload();
});

// "₹ 99,900" and "99900" are the same price.
window.rupees = (v) => String(v ?? '').replace(/[₹,\s]/g, '');


// A readable revision cookie contains no credentials. Compare it on Back and
// when another tab becomes visible, after the session cookies have changed.
const lwSessionVersion = () => document.cookie.split('; ').find((c) => c.startsWith('lw_sv=')) || '';
const lwInitialSessionVersion = lwSessionVersion();
const lwCheckSession = () => {
  if (lwSessionVersion() !== lwInitialSessionVersion) location.reload();
};
window.addEventListener('pageshow', (e) => { if (e.persisted) lwCheckSession(); });
document.addEventListener('visibilitychange', () => { if (!document.hidden) lwCheckSession(); });
document.addEventListener('htmx:beforeRequest', (e) => {
  if (e.detail.requestConfig.path.startsWith('/login/')) lwProcessing('Signing in. Please wait…', false);
});
document.addEventListener('htmx:afterRequest', (e) => {
  if (e.detail.requestConfig.path.startsWith('/login/') && !e.detail.xhr.getResponseHeader('HX-Redirect')) lwProcessing('');
});

// Large, persistent outcomes: users dismiss these when they have read them.
window.lwResult = ({ title, body, danger = false, ok = 'Back to photos' }) => new Promise((resolve) => {
  const dialog = document.getElementById('lw-result');
  dialog.querySelector('[data-result-title]').textContent = title;
  dialog.querySelector('[data-result-body]').textContent = body;
  dialog.querySelector('[data-result-title]').classList.toggle('text-danger', danger);
  dialog.querySelector('button').textContent = ok;
  dialog.addEventListener('close', resolve, { once: true });
  dialog.showModal();
});

// Match the API's 10 MiB image limit; leave room below its 40 MiB body cap.
window.lwPreparePhoto = async (file) => {
  const name = file.name || 'Photo';
  if (!['image/jpeg', 'image/png', 'image/webp', 'image/gif'].includes(file.type)) {
    throw new Error(`“${name}” is not a supported photo. Choose a JPEG, PNG, WebP or GIF image.`);
  }
  if (file.size > 20 * 1024 * 1024) {
    throw new Error(`“${name}” is too large (${(file.size / 1024 / 1024).toFixed(1)} MB). Choose an image under 20 MB. We resize supported photos before uploading.`);
  }
  const preview = URL.createObjectURL(file);
  try {
    const image = new Image();
    image.src = preview;
    try { await image.decode(); } catch { throw new Error(`“${name}” could not be read. Export it again as JPEG, PNG or WebP and try again.`); }
    if (!image.naturalWidth || !image.naturalHeight || image.naturalWidth * image.naturalHeight > 40000000) {
      throw new Error(`“${name}” has dimensions that are too large. Resize it below 40 megapixels and try again.`);
    }
    // Preserve animation; the server still limits GIF files to 10 MiB.
    if (file.type === 'image/gif') {
      if (file.size > 10 * 1024 * 1024) throw new Error(`“${name}” is too large. Choose a GIF under 10 MB or export a still photo.`);
      return file;
    }
    const scale = Math.min(1, 2048 / Math.max(image.naturalWidth, image.naturalHeight));
    const canvas = document.createElement('canvas');
    canvas.width = Math.max(1, Math.round(image.naturalWidth * scale));
    canvas.height = Math.max(1, Math.round(image.naturalHeight * scale));
    const ctx = canvas.getContext('2d');
    if (!ctx) throw new Error('Your browser could not prepare this photo. Choose a smaller image or try another browser.');
    ctx.drawImage(image, 0, 0, canvas.width, canvas.height);
    const blob = await new Promise((resolve) => canvas.toBlob(resolve, 'image/webp', 0.82));
    if (!blob || blob.size > 10 * 1024 * 1024) throw new Error(`“${name}” is still too large after resizing. Choose a smaller image under 10 MB.`);
    if (scale === 1 && file.size <= blob.size) return file;
    const extension = blob.type === 'image/webp' ? 'webp' : 'png';
    return new File([blob], name.replace(/\.[^.]+$/, '') + '.' + extension, { type: blob.type });
  } finally { URL.revokeObjectURL(preview); }
};

window.lwUpload = (method, path, body, onProgress) => new Promise((resolve, reject) => {
  const xhr = new XMLHttpRequest();
  xhr.open(method, path);
  xhr.timeout = 120000;
  xhr.upload.onprogress = (e) => { if (e.lengthComputable) onProgress(Math.round(e.loaded / e.total * 100)); };
  xhr.upload.onload = () => onProgress(100);
  xhr.onerror = () => reject(new Error('The connection was interrupted. Reconnect and check your inventory before trying again.'));
  xhr.ontimeout = () => reject(new Error('The upload took too long. Check your inventory before retrying, or use fewer photos.'));
  xhr.onload = () => {
    let result;
    try { result = JSON.parse(xhr.responseText); } catch {}
    if (xhr.status >= 200 && xhr.status < 300) {
      if (!result) { reject(new Error('The server returned an unexpected response. Check your inventory before retrying.')); return; }
      resolve(result);
    } else {
      reject(new Error(result?.error || (xhr.status === 413 ? 'These photos are too large. Choose fewer or smaller photos and try again.' : xhr.status === 401 ? 'Your session ended. Sign in again before saving.' : 'The upload failed. Your form is still here; try again in a moment.')));
    }
  };
  xhr.send(body);
});
