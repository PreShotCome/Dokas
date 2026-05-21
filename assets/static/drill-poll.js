// Stop the HTMX poll once the drill is terminal. The /drills/{id}/steps
// partial sends an `HX-Trigger: drill-terminal` response header on
// success/failure; we listen for the matching client-side event and strip
// the `hx-trigger` attribute off the polling pane so HTMX gives up.
document.body.addEventListener('drill-terminal', function () {
  var pane = document.getElementById('steps-pane');
  if (pane) {
    pane.removeAttribute('hx-trigger');
    pane.removeAttribute('hx-get');
  }
});
