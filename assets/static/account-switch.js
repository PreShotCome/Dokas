// Submit the account-switcher form as soon as a different account is picked.
// The form also has an explicit "Switch" button as a no-JS fallback; this
// just makes the common case one click. CSP forbids inline handlers, so the
// listener is wired from this external file.
document.addEventListener('DOMContentLoaded', function () {
  document.querySelectorAll('select[data-account-switcher]').forEach(function (sel) {
    sel.addEventListener('change', function () {
      if (sel.form) {
        sel.form.submit();
      }
    });
  });
});
