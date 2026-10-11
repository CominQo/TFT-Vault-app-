// Picks which animation clip plays for each role. Used by the optimizer (to decide what to keep)
// and mirrored in the runtime (cb3d) so both always agree.
export const ROLES = ['idle', 'attack', 'run', 'dance', 'spell'];

const bad = /(to|in)idle|toidle|totransition|spawn|death|die|aggro|stun|recall|knockup|fear|taunt|laugh|cry|joke|channel|loop_end|land|takeoff|^as_alpha|alpha/i;

export function pickClips(clips) {
  // clips: [{name, duration}]
  const by = (re, opts = {}) => {
    const c = clips.filter(x => re.test(x.name) && !(opts.not && opts.not.test(x.name)));
    if (opts.prefer) c.sort((a, b) => (opts.prefer.test(b.name) ? 1 : 0) - (opts.prefer.test(a.name) ? 1 : 0) || (opts.long ? b.duration - a.duration : a.duration - b.duration));
    else if (opts.long) c.sort((a, b) => b.duration - a.duration);
    return c[0];
  };
  const out = {};
  out.idle = by(/idle/i, { not: bad, prefer: /idle1|base|^as_idle$|idle$/i, long: true })
          || by(/idle/i, { not: /(to|in)idle|toidle|spawn/i, long: true })
          || by(/idle/i, { long: true });
  out.attack = by(/attack1|attack|auto/i, { not: /(to|in)idle|toidle|crit|spell|alpha/i, prefer: /attack1|auto$/i });
  out.run = by(/run/i, { not: /(to|in)(idle|run)|toidle|torun|runin/i, prefer: /base|^as_run$/i });
  out.dance = by(/dance|celebration|joke/i, { not: /intro|(to|in)idle/i, prefer: /loop|dance1?$|celebration/i, long: true });
  out.spell = by(/spell|alpha/i, { not: /_?(to|in)_?(idle|run)|toidle|torun|\d+in$|in$|knock|pos[1-9]$/i, prefer: /spell1?$|spell$|spell4$/i, long: true });
  // fall back so every role has something sensible
  out.attack = out.attack || out.spell || out.idle;
  out.spell = out.spell || out.attack;
  out.run = out.run || out.idle;
  out.dance = out.dance || out.spell || out.idle;
  return out;
}
