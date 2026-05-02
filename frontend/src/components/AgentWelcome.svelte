<script>
  import appIcon from '../assets/appicon.png';

  export let userName = '';

  function getAgentGreeting() {
    const hour = new Date().getHours();
    if (hour < 12) return 'Ready to build';
    if (hour < 17) return "Let's get things done";
    return 'Evening mode, activated';
  }

  $: greeting = getAgentGreeting();
  $: displayGreeting = userName ? `${greeting}, ${userName}` : greeting;
</script>

<div class="welcome">
  <h1 class="welcome-heading">
    <img class="welcome-icon" src={appIcon} alt="Flow" />
    <span>{displayGreeting}</span>
  </h1>
</div>

<style>
  .welcome {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    min-height: 50vh;
    text-align: center;
    gap: 24px;
    padding-top: 10vh;
    animation: welcomeFade 0.5s ease;
  }

  .welcome-heading {
    display: flex;
    align-items: center;
    gap: 12px;
    font-size: 36px;
    font-weight: 600;
    color: var(--text-greeting);
    line-height: 1.2;
    letter-spacing: -0.5px;
  }

  .welcome-icon {
    width: 44px;
    height: 44px;
    border-radius: 12px;
    object-fit: cover;
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.5);
  }

  @keyframes welcomeFade {
    from { opacity: 0; transform: translateY(10px); }
    to   { opacity: 1; transform: translateY(0); }
  }
</style>
